// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

var cleanupGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

type cleanupCache struct {
	calls int
	err   error
	gvrs  []schema.GroupVersionResource
}

func (c *cleanupCache) Get(discovery.DiscoveryInterface) ([]schema.GroupVersionResource, error) {
	c.calls++
	return c.gvrs, c.err
}

type cleanupReader struct {
	client.Reader
	current *corev1.Namespace
	err     error
	calls   int
}

func (r *cleanupReader) Get(ctx context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	r.calls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.err != nil {
		return r.err
	}
	r.current.DeepCopyInto(obj.(*corev1.Namespace))
	return nil
}
func cleanupNamespace() *corev1.Namespace {
	stamp := metav1.NewTime(time.Now().Add(-time.Hour))
	return &corev1.Namespace{Name: "tenant-a-ns", UID: "original-ns", DeletionTimestamp: &stamp}
}
func cleanupObject(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"namespace": namespace, "name": name, "uid": "original-object", "resourceVersion": "10", "finalizers": []any{"example.com/hold"}}}}
}

func TestCleanupSkipsObsoleteNamespaces(t *testing.T) {
	for _, name := range []string{"nil", "no-uid", "active", "recreated", "gone", "reassigned", "read-error", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			ns := cleanupNamespace()
			current := ns.DeepCopy()
			reader := &cleanupReader{current: current}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "nil":
				ns = nil
			case "no-uid":
				ns.UID = ""
			case "active":
				ns.DeletionTimestamp = nil
			case "recreated":
				current.UID = "replacement"
				current.DeletionTimestamp = nil
			case "gone":
				reader.err = apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, ns.Name)
			case "reassigned":
				current.OwnerReferences = []metav1.OwnerReference{{Kind: "Tenant", APIVersion: "capsule.clastix.io/v1beta2", Name: "tenant-b", UID: "other-tenant"}}
			case "read-error":
				reader.err = errors.New("reader unavailable")
			case "cancelled":
				cancel()
			}
			cache := &cleanupCache{}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"})
			changed, err := tenant.NamespacedCascadingCleanup(ctx, reader, nil, cache, dyn, ns)
			wantErr := name == "read-error" || name == "cancelled"
			if (err != nil) != wantErr {
				t.Fatalf("error=%v", err)
			}
			if changed || cache.calls != 0 || len(dyn.Actions()) != 0 {
				t.Fatalf("unexpected cleanup: changed=%v discovery=%d actions=%v", changed, cache.calls, dyn.Actions())
			}
		})
	}
}

func TestCleanupRechecksNamespaceAfterList(t *testing.T) {
	for _, name := range []string{"recreated", "gone", "read-error"} {
		t.Run(name, func(t *testing.T) {
			ns := cleanupNamespace()
			reader := &cleanupReader{current: ns.DeepCopy()}
			cache := &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}
			obj := cleanupObject(ns.Name, "new-object")
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"}, obj)
			dyn.PrependReactor("list", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
				switch name {
				case "recreated":
					reader.current.UID = "new-namespace"
					reader.current.DeletionTimestamp = nil
				case "gone":
					reader.err = apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, ns.Name)
				case "read-error":
					reader.err = errors.New("reader unavailable")
				}
				return false, nil, nil
			})
			_, err := tenant.NamespacedCascadingCleanup(context.Background(), reader, nil, cache, dyn, ns)
			if (err != nil) != (name == "read-error") {
				t.Fatalf("error=%v", err)
			}
			for _, a := range dyn.Actions() {
				if a.GetVerb() != "list" {
					t.Fatalf("stale cleanup performed %s", a.GetVerb())
				}
			}
		})
	}
}

func TestCleanupUsesObjectPreconditionsAndPreservesOtherNamespaces(t *testing.T) {
	for _, mode := range []string{"normal", "replaced-after-delete", "replaced-namespace-before-patch", "delete-conflict", "patch-conflict", "no-uid", "no-version"} {
		t.Run(mode, func(t *testing.T) {
			ns := cleanupNamespace()
			reader := &cleanupReader{current: ns.DeepCopy()}
			obj := cleanupObject(ns.Name, "held")
			other := cleanupObject("tenant-b-ns", "held")
			if mode == "no-uid" {
				obj.SetUID("")
			}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"}, obj, other)
			deletes, patches := 0, 0
			dyn.PrependReactor("delete", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				deletes++
				opts := a.(ktesting.DeleteAction).GetDeleteOptions()
				if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != obj.GetUID() {
					t.Fatal("delete lacks object UID precondition")
				}
				if mode == "delete-conflict" {
					return true, nil, apierrors.NewConflict(cleanupGVR.GroupResource(), obj.GetName(), errors.New("UID changed"))
				}
				current := obj.DeepCopy()
				stamp := metav1.Now()
				current.SetDeletionTimestamp(&stamp)
				current.SetResourceVersion("11")
				if mode == "replaced-after-delete" {
					current.SetUID("replacement-object")
					current.SetDeletionTimestamp(nil)
				}
				if mode == "no-version" {
					current.SetResourceVersion("")
				}
				if mode == "replaced-namespace-before-patch" {
					reader.current.UID = "replacement-namespace"
				}
				if err := dyn.Tracker().Update(cleanupGVR, current, ns.Name); err != nil {
					t.Fatal(err)
				}
				return true, nil, nil
			})
			dyn.PrependReactor("patch", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				patches++
				var patch struct {
					Metadata struct {
						UID        types.UID `json:"uid"`
						RV         string    `json:"resourceVersion"`
						Finalizers []string  `json:"finalizers"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal(a.(ktesting.PatchAction).GetPatch(), &patch); err != nil {
					t.Fatal(err)
				}
				if patch.Metadata.UID != obj.GetUID() || patch.Metadata.RV != "11" || patch.Metadata.Finalizers == nil || len(patch.Metadata.Finalizers) != 0 {
					t.Fatalf("unsafe patch: %+v", patch)
				}
				if mode == "patch-conflict" {
					return true, nil, apierrors.NewConflict(cleanupGVR.GroupResource(), obj.GetName(), errors.New("version changed"))
				}
				return false, nil, nil
			})
			changed, err := tenant.NamespacedCascadingCleanup(context.Background(), reader, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}, dyn, ns)
			wantErr := strings.Contains(mode, "conflict") || mode == "no-uid" || mode == "no-version"
			if (err != nil) != wantErr {
				t.Fatalf("error=%v", err)
			}
			wantPatches := 0
			if mode == "normal" || mode == "patch-conflict" {
				wantPatches = 1
			}
			if patches != wantPatches {
				t.Fatalf("patches=%d want=%d", patches, wantPatches)
			}
			if mode == "no-uid" && deletes != 0 {
				t.Fatal("deleted object without UID")
			}
			if mode == "normal" && !changed {
				t.Fatal("cleanup did not report changes")
			}
			untouched, err := dyn.Resource(cleanupGVR).Namespace("tenant-b-ns").Get(context.Background(), "held", metav1.GetOptions{})
			if err != nil || !reflect.DeepEqual(untouched, other) {
				t.Fatalf("other Tenant affected: %v", err)
			}
		})
	}
}

func TestCleanupErrorsAndPods(t *testing.T) {
	for _, mode := range []string{"discovery", "forbidden", "missing-kind", "pods"} {
		t.Run(mode, func(t *testing.T) {
			ns := cleanupNamespace()
			reader := &cleanupReader{current: ns.DeepCopy()}
			cache := &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"})
			switch mode {
			case "discovery":
				cache.err = errors.New("discovery failed")
			case "pods":
				cache.gvrs = []schema.GroupVersionResource{{Version: "v1", Resource: "pods"}}
			default:
				dyn.PrependReactor("list", "*", func(ktesting.Action) (bool, runtime.Object, error) {
					if mode == "missing-kind" {
						return true, nil, apierrors.NewNotFound(cleanupGVR.GroupResource(), "")
					}
					return true, nil, apierrors.NewForbidden(cleanupGVR.GroupResource(), "", errors.New("not allowed"))
				})
			}
			_, err := tenant.NamespacedCascadingCleanup(context.Background(), reader, nil, cache, dyn, ns)
			if (err != nil) != (mode == "discovery" || mode == "forbidden") {
				t.Fatalf("error=%v", err)
			}
			if mode == "pods" && len(dyn.Actions()) != 0 {
				t.Fatal("cleanup must not access Pods")
			}
		})
	}
}

func TestCleanupContinuesAfterAnObjectError(t *testing.T) {
	ns := cleanupNamespace()
	first, second := cleanupObject(ns.Name, "a-conflict"), cleanupObject(ns.Name, "b-cleanup")
	second.SetFinalizers(nil)
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"}, first, second)
	dyn.PrependReactor("delete", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
		if a.(ktesting.DeleteAction).GetName() == first.GetName() {
			return true, nil, apierrors.NewConflict(cleanupGVR.GroupResource(), first.GetName(), errors.New("object changed"))
		}
		return false, nil, nil
	})
	changed, err := tenant.NamespacedCascadingCleanup(context.Background(), &cleanupReader{current: ns}, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}, dyn, ns)
	if !changed || err == nil || !strings.Contains(err.Error(), first.GetName()) {
		t.Fatalf("changed=%v error=%v", changed, err)
	}
	if _, err := dyn.Resource(cleanupGVR).Namespace(ns.Name).Get(context.Background(), second.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("unrelated object was not cleaned up: %v", err)
	}
}

func TestCleanupEmptyListsDoNotRequireRepeatedNamespaceReads(t *testing.T) {
	ns := cleanupNamespace()
	reader := &cleanupReader{current: ns}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"})
	changed, err := tenant.NamespacedCascadingCleanup(context.Background(), reader, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}, dyn, ns)
	if changed || err != nil || reader.calls != 1 {
		t.Fatalf("changed=%v error=%v namespace GETs=%d", changed, err, reader.calls)
	}
}

type blockedCleanupDynamic struct {
	dynamic.Interface
	started chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
}

func (d *blockedCleanupDynamic) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return blockedCleanupNamespaceable{NamespaceableResourceInterface: d.Interface.Resource(gvr), probe: d}
}

type blockedCleanupNamespaceable struct {
	dynamic.NamespaceableResourceInterface
	probe *blockedCleanupDynamic
}

func (r blockedCleanupNamespaceable) Namespace(namespace string) dynamic.ResourceInterface {
	return blockedCleanupResource{ResourceInterface: r.NamespaceableResourceInterface.Namespace(namespace), probe: r.probe}
}

type blockedCleanupResource struct {
	dynamic.ResourceInterface
	probe *blockedCleanupDynamic
}

func (r blockedCleanupResource) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	active := r.probe.active.Add(1)
	defer r.probe.active.Add(-1)
	for previous := r.probe.maximum.Load(); active > previous; previous = r.probe.maximum.Load() {
		if r.probe.maximum.CompareAndSwap(previous, active) {
			break
		}
	}
	r.probe.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCleanupBoundsConcurrencyAndHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ns := cleanupNamespace()
	dyn := &blockedCleanupDynamic{Interface: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), started: make(chan struct{}, 8)}
	cache := &cleanupCache{}
	for i := 0; i < 8; i++ {
		cache.gvrs = append(cache.gvrs, schema.GroupVersionResource{Version: "v1", Resource: fmt.Sprintf("resources%d", i)})
	}
	done := make(chan error, 1)
	go func() {
		_, err := tenant.NamespacedCascadingCleanup(ctx, &cleanupReader{current: ns}, nil, cache, dyn, ns)
		done <- err
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-dyn.started:
		case <-ctx.Done():
			t.Fatal("cleanup did not start its bounded workers")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup did not preserve cancellation: %v", err)
	}
	if dyn.maximum.Load() != 4 || dyn.active.Load() != 0 {
		t.Fatalf("maximum concurrent requests=%d remaining=%d", dyn.maximum.Load(), dyn.active.Load())
	}
}

func BenchmarkNamespaceCleanup(b *testing.B) {
	for _, count := range []int{30, 300} {
		b.Run(fmt.Sprintf("resourceTypes=%d", count), func(b *testing.B) {
			ns := cleanupNamespace()
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			ns.Finalizers = []string{"example.com/hold"}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
			cache := &cleanupCache{}
			kinds := map[schema.GroupVersionResource]string{}
			for i := 0; i < count; i++ {
				gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: fmt.Sprintf("objects%d", i)}
				cache.gvrs = append(cache.gvrs, gvr)
				kinds[gvr] = fmt.Sprintf("Object%dList", i)
			}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				dyn.ClearActions()
				if _, err := tenant.NamespacedCascadingCleanup(context.Background(), reader, nil, cache, dyn, ns); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(count), "dynamic-LIST/op")
			b.ReportMetric(1, "namespace-GET/op")
		})
	}
}

// Measure the real cleanup loop with populated LISTs.
// In-memory responses omit network latency and server-side processing.
type populatedCleanupDynamic struct {
	dynamic.Interface
	resources *populatedCleanupResource
}

func (d populatedCleanupDynamic) Resource(schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return populatedCleanupNamespaceable{resources: d.resources}
}

type populatedCleanupNamespaceable struct {
	dynamic.NamespaceableResourceInterface
	resources *populatedCleanupResource
}

func (d populatedCleanupNamespaceable) Namespace(namespace string) dynamic.ResourceInterface {
	if namespace != "tenant-a-ns" {
		panic("unexpected namespace")
	}
	return d.resources
}

type populatedCleanupResource struct {
	dynamic.ResourceInterface
	list                   *unstructured.UnstructuredList
	objects                map[string]*unstructured.Unstructured
	gets, deletes, patches int
}

func (r *populatedCleanupResource) List(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return r.list.DeepCopy(), nil
}
func (r *populatedCleanupResource) Get(_ context.Context, name string, _ metav1.GetOptions, _ ...string) (*unstructured.Unstructured, error) {
	r.gets++
	return r.objects[name].DeepCopy(), nil
}
func (r *populatedCleanupResource) Delete(_ context.Context, name string, opts metav1.DeleteOptions, _ ...string) error {
	if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != r.objects[name].GetUID() {
		return errors.New("missing UID precondition")
	}
	r.deletes++
	return nil
}
func (r *populatedCleanupResource) Patch(_ context.Context, name string, _ types.PatchType, _ []byte, _ metav1.PatchOptions, _ ...string) (*unstructured.Unstructured, error) {
	r.patches++
	return r.objects[name].DeepCopy(), nil
}
func BenchmarkNamespaceCleanupPopulated(b *testing.B) {
	for _, mode := range []string{"already-deleting-no-finalizers", "already-deleting-with-finalizers", "needs-delete", "capsule-lifecycle"} {
		for _, count := range []int{1, 100, 1000} {
			b.Run(fmt.Sprintf("%s/objects=%d", mode, count), func(b *testing.B) {
				ns := cleanupNamespace()
				gvr := cleanupGVR
				if mode == "capsule-lifecycle" {
					gvr = capsulev1beta2.GroupVersion.WithResource("resourcepermits")
				}
				reader := &cleanupReader{current: ns}
				resources := &populatedCleanupResource{list: &unstructured.UnstructuredList{}, objects: map[string]*unstructured.Unstructured{}}
				for i := 0; i < count; i++ {
					obj := cleanupObject(ns.Name, fmt.Sprintf("object-%d", i))
					obj.SetUID(types.UID(obj.GetName()))
					if mode != "needs-delete" {
						stamp := metav1.Now()
						obj.SetDeletionTimestamp(&stamp)
					}
					if mode != "already-deleting-with-finalizers" {
						obj.SetFinalizers(nil)
					}
					if mode == "capsule-lifecycle" {
						obj.SetAPIVersion(gvr.GroupVersion().String())
						obj.SetKind("ResourcePermit")
						obj.SetFinalizers([]string{meta.ControllerFinalizer})
					}
					resources.list.Items = append(resources.list.Items, *obj)
					resources.objects[obj.GetName()] = obj
				}
				dyn := populatedCleanupDynamic{resources: resources}
				cache := &cleanupCache{gvrs: []schema.GroupVersionResource{gvr}}
				b.ReportAllocs()
				for b.Loop() {
					// Responses are immutable snapshots, so every iteration has the same work.
					changed, err := tenant.NamespacedCascadingCleanup(b.Context(), reader, nil, cache, dyn, ns)
					if err != nil || changed != (mode == "needs-delete" || mode == "already-deleting-with-finalizers") {
						b.Fatalf("changed=%v err=%v", changed, err)
					}
				}
				if mode == "needs-delete" && resources.deletes != count*b.N {
					b.Fatal("missing DELETEs")
				}
				if mode == "already-deleting-with-finalizers" && resources.patches != count*b.N {
					b.Fatal("missing PATCHes")
				}
				if resources.gets != 0 {
					b.Fatal("cleanup performed redundant object GETs")
				}
				b.ReportMetric(float64(reader.calls)/float64(b.N), "namespace-GET/op")
				b.ReportMetric(float64(resources.gets)/float64(b.N), "object-GET/op")
				b.ReportMetric(float64(resources.deletes)/float64(b.N), "DELETE/op")
				b.ReportMetric(float64(resources.patches)/float64(b.N), "PATCH/op")
			})
		}
	}
}

func TestCleanupPreservesCapsuleLifecycleFinalizers(t *testing.T) {
	for _, mode := range []string{"permit", "permit-mixed", "legacy-resource", "added-after-list", "foreign-api"} {
		t.Run(mode, func(t *testing.T) {
			ns := cleanupNamespace()
			gvr := capsulev1beta2.GroupVersion.WithResource("resourcepermits")
			obj := cleanupObject(ns.Name, "held")
			obj.SetAPIVersion(gvr.GroupVersion().String())
			obj.SetKind("ResourcePermit")
			obj.SetDeletionTimestamp(ns.DeletionTimestamp)
			obj.SetFinalizers([]string{meta.ControllerFinalizer})
			want := []string{meta.ControllerFinalizer}
			switch mode {
			case "permit-mixed":
				obj.SetFinalizers([]string{meta.ControllerFinalizer, "example.com/hold"})
			case "legacy-resource":
				gvr.Resource = "tenantresources"
				obj.SetKind("TenantResource")
				obj.SetFinalizers([]string{meta.LegacyResourceFinalizer, "example.com/hold"})
				want = []string{meta.LegacyResourceFinalizer}
			case "added-after-list":
				obj.SetFinalizers([]string{"example.com/hold"})
			case "foreign-api":
				gvr = cleanupGVR
				obj.SetAPIVersion("v1")
				obj.SetKind("ConfigMap")
				want = nil
			}
			foreign := obj.DeepCopy()
			foreign.SetNamespace("tenant-b-ns")
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: obj.GetKind() + "List"}, obj, foreign)
			if mode == "added-after-list" {
				listed := false
				dyn.PrependReactor("list", gvr.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
					if listed {
						return false, nil, nil
					}
					listed = true
					current := obj.DeepCopy()
					current.SetFinalizers([]string{meta.ControllerFinalizer, "example.com/hold"})
					current.SetResourceVersion("11")
					if err := dyn.Tracker().Update(gvr, current, ns.Name); err != nil {
						t.Fatal(err)
					}
					return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*obj.DeepCopy()}}, nil
				})
			}
			patches := 0
			dyn.PrependReactor("patch", gvr.Resource, func(a ktesting.Action) (bool, runtime.Object, error) {
				patches++
				var patch struct {
					Metadata struct {
						Finalizers      []string  `json:"finalizers"`
						UID             types.UID `json:"uid"`
						ResourceVersion string    `json:"resourceVersion"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal(a.(ktesting.PatchAction).GetPatch(), &patch); err != nil {
					return true, nil, err
				}
				if patch.Metadata.UID != obj.GetUID() || patch.Metadata.ResourceVersion == "" {
					return true, nil, errors.New("patch lacks object preconditions")
				}
				if mode == "added-after-list" && patch.Metadata.ResourceVersion == "10" {
					return true, nil, apierrors.NewConflict(gvr.GroupResource(), obj.GetName(), errors.New("finalizers changed after LIST"))
				}
				if !reflect.DeepEqual(patch.Metadata.Finalizers, want) && !(len(want) == 0 && len(patch.Metadata.Finalizers) == 0) {
					return true, nil, fmt.Errorf("finalizers=%v, want=%v", patch.Metadata.Finalizers, want)
				}
				return false, nil, nil
			})
			reader := &cleanupReader{current: ns}
			changed, err := tenant.NamespacedCascadingCleanup(t.Context(), reader, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{gvr}}, dyn, ns)
			if mode == "added-after-list" {
				if changed || !apierrors.IsConflict(err) {
					t.Fatalf("stale snapshot was accepted: changed=%v error=%v", changed, err)
				}
				current, getErr := dyn.Tracker().Get(gvr, ns.Name, obj.GetName())
				if getErr != nil || !reflect.DeepEqual(current.(*unstructured.Unstructured).GetFinalizers(), []string{meta.ControllerFinalizer, "example.com/hold"}) {
					t.Fatalf("stale patch changed finalizers: %v, %v", current, getErr)
				}
				patches = 0
				changed, err = tenant.NamespacedCascadingCleanup(t.Context(), reader, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{gvr}}, dyn, ns)
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "permit" {
				if changed || patches != 0 || reader.calls != 1 {
					t.Fatalf("unchanged permit: changed=%v patches=%d namespace GETs=%d", changed, patches, reader.calls)
				}
			} else if !changed || patches != 1 {
				t.Fatalf("changed=%v patches=%d", changed, patches)
			}
			remaining, err := dyn.Tracker().Get(gvr, foreign.GetNamespace(), foreign.GetName())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(remaining.(*unstructured.Unstructured).GetFinalizers(), foreign.GetFinalizers()) {
				t.Fatal("changed another tenant's finalizers")
			}
		})
	}
}

func TestCleanupChecksNamespaceOnlyForWrites(t *testing.T) {
	for _, mode := range []string{"no-finalizers", "foreign-finalizer", "capsule-finalizer", "delete"} {
		t.Run(mode, func(t *testing.T) {
			ns := cleanupNamespace()
			reader := &cleanupReader{current: ns}
			gvr := cleanupGVR
			obj := cleanupObject(ns.Name, "held")
			obj.SetDeletionTimestamp(ns.DeletionTimestamp)
			expectedReads := 1
			switch mode {
			case "no-finalizers":
				obj.SetFinalizers(nil)
			case "foreign-finalizer":
				expectedReads = 2
			case "delete":
				obj.SetDeletionTimestamp(nil)
				obj.SetFinalizers(nil)
				expectedReads = 2
			case "capsule-finalizer":
				gvr = capsulev1beta2.GroupVersion.WithResource("resourcepermits")
				obj.SetAPIVersion(gvr.GroupVersion().String())
				obj.SetKind("ResourcePermit")
				obj.SetFinalizers([]string{meta.ControllerFinalizer})
			}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: obj.GetKind() + "List"}, obj)
			_, err := tenant.NamespacedCascadingCleanup(t.Context(), reader, nil, &cleanupCache{gvrs: []schema.GroupVersionResource{gvr}}, dyn, ns)
			if err != nil {
				t.Fatal(err)
			}
			if reader.calls != expectedReads {
				t.Fatalf("namespace GETs=%d, want=%d", reader.calls, expectedReads)
			}
			for _, action := range dyn.Actions() {
				if action.GetVerb() == "get" {
					t.Fatal("cleanup performed a redundant object GET")
				}
			}
		})
	}
}

func cleanupContentConditions(message string) []corev1.NamespaceCondition {
	return []corev1.NamespaceCondition{
		{Type: corev1.NamespaceDeletionDiscoveryFailure, Status: corev1.ConditionFalse},
		{Type: corev1.NamespaceDeletionGVParsingFailure, Status: corev1.ConditionFalse},
		{Type: corev1.NamespaceDeletionContentFailure, Status: corev1.ConditionFalse},
		{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now(), Message: message},
		{Type: corev1.NamespaceFinalizersRemaining, Status: corev1.ConditionTrue},
	}
}

func TestCleanupUsesRecentNativeContentReportsWithFullScanFallback(t *testing.T) {
	for _, mode := range []string{"hint", "complete", "stale", "future", "before-deletion", "missing", "discovery-error", "parsing-error", "deletion-error", "unknown-status", "unknown-format", "malformed-count", "zero-count", "missing-group", "unknown-resource", "inconsistent-complete"} {
		t.Run(mode, func(t *testing.T) {
			ns := cleanupNamespace()
			ns.Status.Conditions = cleanupContentConditions("Some resources are remaining: configmaps. has 1 resource instances")
			switch mode {
			case "complete", "inconsistent-complete":
				ns.Status.Conditions[3].Status = corev1.ConditionFalse
				if mode == "complete" {
					ns.Status.Conditions[4].Status = corev1.ConditionFalse
				}
			case "stale":
				ns.Status.Conditions[3].LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Minute))
			case "future":
				ns.Status.Conditions[3].LastTransitionTime = metav1.NewTime(time.Now().Add(time.Minute))
			case "before-deletion":
				ns.DeletionTimestamp = new(metav1.Now())
				ns.Status.Conditions[3].LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Second))
			case "missing":
				ns.Status.Conditions = nil
			case "discovery-error":
				ns.Status.Conditions[0].Status = corev1.ConditionTrue
			case "parsing-error":
				ns.Status.Conditions[1].Status = corev1.ConditionUnknown
			case "deletion-error":
				ns.Status.Conditions[2].Status = corev1.ConditionTrue
			case "unknown-status":
				ns.Status.Conditions[3].Status = corev1.ConditionUnknown
			case "unknown-format":
				ns.Status.Conditions[3].Message = "Unrecognized remaining resources report"
			case "malformed-count":
				ns.Status.Conditions[3].Message = "Some resources are remaining: configmaps. has many resource instances"
			case "zero-count":
				ns.Status.Conditions[3].Message = "Some resources are remaining: configmaps. has 0 resource instances"
			case "missing-group":
				ns.Status.Conditions[3].Message = "Some resources are remaining: configmaps has 1 resource instances"
			case "unknown-resource":
				ns.Status.Conditions[3].Message = "Some resources are remaining: unavailable.example.com has 1 resource instances"
			}
			widgetGVR := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
			widget := cleanupObject(ns.Name, "fallback")
			widget.SetAPIVersion("example.com/v1")
			widget.SetKind("Widget")
			widget.SetFinalizers(nil)
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList", widgetGVR: "WidgetList"}, widget)
			cache := &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR, widgetGVR}}
			_, err := tenant.NamespacedCascadingCleanup(t.Context(), &cleanupReader{current: ns}, nil, cache, dyn, ns)
			if err != nil {
				t.Fatal(err)
			}
			lists := 0
			for _, action := range dyn.Actions() {
				if action.GetVerb() == "list" {
					lists++
				}
			}
			wantLists := 2
			if mode == "hint" {
				wantLists = 1
			}
			if mode == "complete" {
				wantLists = 0
			}
			if lists != wantLists {
				t.Fatalf("LISTs=%d want=%d", lists, wantLists)
			}
			if mode == "complete" && cache.calls != 0 {
				t.Fatal("completed content unnecessarily used discovery")
			}
			_, err = dyn.Tracker().Get(widgetGVR, ns.Name, widget.GetName())
			if apierrors.IsNotFound(err) != (wantLists == 2) {
				t.Fatalf("fallback resource error=%v", err)
			}
			if mode == "hint" || mode == "complete" {
				// A stale or incomplete report can delay work, but cannot abandon
				// it: once the hint expires, even unchanged status scans all APIs.
				ns.Status.Conditions[3].LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Minute))
				if _, err := tenant.NamespacedCascadingCleanup(t.Context(), &cleanupReader{current: ns}, nil, cache, dyn, ns); err != nil {
					t.Fatal(err)
				}
				if _, err := dyn.Tracker().Get(widgetGVR, ns.Name, widget.GetName()); !apierrors.IsNotFound(err) {
					t.Fatalf("expired hint did not clean unreported content: %v", err)
				}
			}
		})
	}
}

func BenchmarkNamespaceCleanupContentReports(b *testing.B) {
	for _, count := range []int{30, 300} {
		for _, mode := range []string{"full", "hint", "complete"} {
			b.Run(fmt.Sprintf("resourceTypes=%d/%s", count, mode), func(b *testing.B) {
				ns := cleanupNamespace()
				if mode != "full" {
					ns.Status.Conditions = cleanupContentConditions("Some resources are remaining: configmaps. has 1 resource instances")
				}
				if mode == "complete" {
					ns.Status.Conditions[3].Status = corev1.ConditionFalse
					ns.Status.Conditions[4].Status = corev1.ConditionFalse
				}
				cache := &cleanupCache{gvrs: []schema.GroupVersionResource{cleanupGVR}}
				kinds := map[schema.GroupVersionResource]string{cleanupGVR: "ConfigMapList"}
				for i := 1; i < count; i++ {
					gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: fmt.Sprintf("objects%d", i)}
					cache.gvrs = append(cache.gvrs, gvr)
					kinds[gvr] = fmt.Sprintf("Object%dList", i)
				}
				dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), kinds)
				lists := 0
				b.ReportAllocs()
				for b.Loop() {
					dyn.ClearActions()
					if changed, err := tenant.NamespacedCascadingCleanup(b.Context(), &cleanupReader{current: ns}, nil, cache, dyn, ns); err != nil || changed {
						b.Fatalf("changed=%v error=%v", changed, err)
					}
					for _, action := range dyn.Actions() {
						if action.GetVerb() == "list" {
							lists++
						}
					}
				}
				wantLists := count
				if mode == "hint" {
					wantLists = 1
				}
				if mode == "complete" {
					wantLists = 0
				}
				if lists != wantLists*b.N {
					b.Fatalf("LISTs=%d want=%d", lists, wantLists*b.N)
				}
				b.ReportMetric(float64(lists)/float64(b.N), "dynamic-LIST/op")
			})
		}
	}
}
