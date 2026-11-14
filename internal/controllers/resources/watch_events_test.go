// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

var configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

type watchNotifications struct {
	mu      sync.Mutex
	targets map[types.UID]int
}

func (n *watchNotifications) Notify(target replication.Target, _ time.Duration) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.targets[target.UID]++
	return true
}

func (n *watchNotifications) take() map[types.UID]int {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := maps.Clone(n.targets)
	clear(n.targets)
	return out
}

type watchFixture struct {
	watcher       *ResourceWatches
	client        client.WithWatch
	notifications *watchNotifications
	lists, gets   int
}

func watchFixtureFor(tb testing.TB, unrelated int) *watchFixture {
	tb.Helper()
	scheme := runtime.NewScheme()
	_ = capsulev1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	trigger := capsulev1beta2.TenantResourceTrigger{Name: "changes", Operations: []admissionv1.OperationType{admissionv1.OperationAll}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"source": "yes"}}}
	spec := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}
	a := &capsulev1beta2.TenantResource{Name: "a", Namespace: "a", UID: "local-a", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: *spec.DeepCopy()}}
	b := a.DeepCopy()
	b.Name = "b"
	b.Namespace = "b"
	b.UID = "local-b"
	global := &capsulev1beta2.GlobalTenantResource{Name: "global", UID: "global", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: *spec.DeepCopy()}}
	global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "prod"}}
	objects := []client.Object{a, b, global, &corev1.Namespace{Name: "a", UID: "ns-a", Labels: map[string]string{"profile": "prod"}}, &corev1.Namespace{Name: "b", UID: "ns-b", Labels: map[string]string{"profile": "dev"}}}
	for i := range unrelated {
		other := b.DeepCopy()
		other.Name = fmt.Sprintf("unrelated-%d", i)
		other.UID = types.UID(other.Name)
		other.Spec.Triggers[0].Resources = []string{"secrets"}
		objects = append(objects, other)
	}
	f := &watchFixture{notifications: &watchNotifications{targets: map[types.UID]int{}}}
	f.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(a, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(global, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			options := &client.ListOptions{}
			for _, o := range opts {
				o.ApplyToList(options)
			}
			if options.FieldSelector == nil {
				tb.Fatal("unindexed subscription scan")
			}
			f.lists++
			return c.List(ctx, list, opts...)
		},
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			f.gets++
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), apimeta.RESTScopeNamespace)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), apimeta.RESTScopeNamespace)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), apimeta.RESTScopeRoot)
	compiler, _ := cache.NewCELCache()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{configMapsGVR: "ConfigMapList", {Version: "v1", Resource: "secrets"}: "SecretList"})
	f.watcher = NewResourceWatches(f.client, dyn, mapper, logr.Discard(), compiler, cache.NewLabelSelectorCache(), cache.NewPreparedReplicationTriggers(), f.notifications, metrics.NewTriggerRecorder())
	if _, err := f.watcher.Reconcile(tb.Context(), reconcile.Request{}); err != nil {
		tb.Fatal(err)
	}
	f.lists = 0
	f.gets = 0
	return f
}

func sourceObject(namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "source", "namespace": namespace, "uid": "source-uid", "resourceVersion": "1", "labels": map[string]any{"source": "yes"}}, "data": map[string]any{"value": "old"}}}
	return object
}

func TestWatchEventsSelectorsOperationsAndIsolation(t *testing.T) {
	for _, operation := range []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
		t.Run(string(operation), func(t *testing.T) {
			f := watchFixtureFor(t, 100)
			object := sourceObject("a")
			old := object.DeepCopy()
			object.SetResourceVersion("2")
			var before, after any = old, object
			if operation == admissionv1.Create {
				before = nil
			}
			if operation == admissionv1.Delete {
				after = nil
				before = clientcache.DeletedFinalStateUnknown{Obj: old}
			}
			f.watcher.enqueueEvent(t.Context(), configMapsGVR, operation, before, after)
			f.watcher.matchEvent(t.Context(), <-f.watcher.events)
			targets := f.notifications.take()
			if targets["local-a"] != 1 || targets["global"] != 1 || len(targets) != 2 {
				t.Fatalf("wrong targets: %v", targets)
			}
			if f.lists != 2 || f.gets != 1 {
				t.Fatalf("LIST=%d GET=%d", f.lists, f.gets)
			}
			object = sourceObject("b")
			f.watcher.enqueueEvent(t.Context(), configMapsGVR, admissionv1.Create, nil, object)
			f.watcher.matchEvent(t.Context(), <-f.watcher.events)
			targets = f.notifications.take()
			if len(targets) != 1 || targets["local-b"] != 1 {
				t.Fatalf("cross-tenant leak: %v", targets)
			}
		})
	}
}

func TestWatchEventSkipsAndBookkeeping(t *testing.T) {
	for _, kind := range []string{"managed", "managed-old", "ssa", "owner", "selector", "same-version", "nil", "no-namespace", "bookkeeping", "status", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			f := watchFixtureFor(t, 0)
			object := sourceObject("a")
			old := object.DeepCopy()
			object.SetResourceVersion("2")
			resource := configMapsGVR
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "managed":
				object.SetLabels(map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController})
			case "managed-old":
				old.SetLabels(map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController})
			case "ssa":
				object.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: meta.FieldManagerCapsulePrefix + "/resource"}})
			case "owner":
				object.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "a", UID: "tenant"}})
			case "selector":
				object.SetLabels(nil)
				old.SetLabels(nil)
			case "same-version":
				object.SetResourceVersion(old.GetResourceVersion())
			case "nil":
				object = nil
				old = nil
			case "no-namespace":
				object.SetNamespace("")
			case "bookkeeping", "status":
				resource = schema.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Resource: "tenantresources"}
				if kind == "bookkeeping" {
					object.SetAnnotations(map[string]string{replication.TriggerAnnotation: "internal"})
				} else {
					object.Object["status"] = map[string]any{"observedGeneration": int64(1)}
				}
			case "canceled":
				cancel()
			}
			f.watcher.enqueueEvent(ctx, resource, admissionv1.Update, old, object)
			if len(f.watcher.events) > 0 {
				f.watcher.matchEvent(t.Context(), <-f.watcher.events)
			}
			if len(f.notifications.take()) != 0 {
				t.Fatal("ineligible event notified a target")
			}
			if kind != "selector" && (f.lists != 0 || f.gets != 0) {
				t.Fatalf("skip read LIST=%d GET=%d", f.lists, f.gets)
			}
		})
	}
}

func TestWatchSubscriptionUpdatesAndRemoval(t *testing.T) {
	f := watchFixtureFor(t, 0)
	source := &capsulev1beta2.TenantResource{}
	if err := f.client.Get(t.Context(), client.ObjectKey{Name: "a", Namespace: "a"}, source); err != nil {
		t.Fatal(err)
	}
	source.Generation++
	source.Spec.Triggers[0].ObjectSelector.MatchLabels["source"] = "updated"
	if err := f.client.Update(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	object := sourceObject("a")
	f.watcher.enqueueEvent(t.Context(), configMapsGVR, admissionv1.Create, nil, object)
	f.watcher.matchEvent(t.Context(), <-f.watcher.events)
	if targets := f.notifications.take(); targets["local-a"] != 0 || targets["global"] != 1 {
		t.Fatalf("stale selector used: %v", targets)
	}
	source.Spec.Cordoned = new(true)
	source.Generation++
	if err := f.client.Update(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.watcher.prepared.Get(source.UID, source.Generation); ok {
		t.Fatal("cordoned subscription retained")
	}
	if len(f.watcher.desired) != 1 {
		t.Fatal("shared informer was removed while still subscribed")
	}
}

func BenchmarkWatchEventMatching(b *testing.B) {
	for _, unrelated := range []int{0, 100, 1000} {
		b.Run(fmt.Sprintf("unrelated=%d", unrelated), func(b *testing.B) {
			f := watchFixtureFor(b, unrelated)
			object := sourceObject("a")
			event := resourceWatchEvent{done: b.Context().Done(), resource: configMapsGVR, operation: admissionv1.Create, object: object}
			b.ReportAllocs()
			for b.Loop() {
				f.watcher.matchEvent(b.Context(), event)
				f.notifications.take()
			}
			b.ReportMetric(float64(f.lists)/float64(b.N), "LIST/op")
			b.ReportMetric(float64(f.gets)/float64(b.N), "GET/op")
		})
	}
}

func TestWatchDiscoveryAndReadFailures(t *testing.T) {
	f := watchFixtureFor(t, 0)
	source := &capsulev1beta2.TenantResource{}
	if err := f.client.Get(t.Context(), client.ObjectKey{Name: "a", Namespace: "a"}, source); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"nodes", "unknownresources", "configmap"} {
		source.Spec.Triggers[0].Resources = []string{resource}
		source.Generation++
		if err := f.client.Update(t.Context(), source); err != nil {
			t.Fatal(err)
		}
		if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
			t.Fatal(err)
		}
		for key := range f.watcher.desired {
			if key.resource.Resource == resource {
				t.Fatal("unsupported resource watch started")
			}
		}
	}
	failure := fmt.Errorf("cache unavailable")
	f.watcher.client = interceptor.NewClient(f.client, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return failure }})
	object := sourceObject("a")
	f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, operation: admissionv1.Create, object: object})
	if len(f.notifications.take()) != 0 {
		t.Fatal("lookup failure produced a notification")
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != failure {
		t.Fatal("configuration read error lost")
	}
}

func TestWatchQueueHasBoundedSaturation(t *testing.T) {
	f := watchFixtureFor(t, 0)
	object := sourceObject("a")
	for range cap(f.watcher.events) + 10 {
		f.watcher.enqueueEvent(t.Context(), configMapsGVR, admissionv1.Create, nil, object)
	}
	if len(f.watcher.events) != cap(f.watcher.events) {
		t.Fatal("event queue capacity not enforced")
	}
	if len(f.notifications.take()) != 0 {
		t.Fatal("enqueue performed notification work synchronously")
	}
}

func TestLocalWatchCountIsBounded(t *testing.T) {
	f := watchFixtureFor(t, 0)
	if err := f.client.Delete(t.Context(), &capsulev1beta2.GlobalTenantResource{Name: "global"}); err != nil {
		t.Fatal(err)
	}
	for i := range maxResourceWatches + 10 {
		name := fmt.Sprintf("namespace-%03d", i)
		source := &capsulev1beta2.TenantResource{Name: "source", Namespace: name, UID: types.UID(name), Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}}}}}
		if err := f.client.Create(t.Context(), source); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(f.watcher.desired) != maxResourceWatches {
		t.Fatalf("stream limit: %d", len(f.watcher.desired))
	}
	for key := range f.watcher.desired {
		if key.namespace == "" {
			t.Fatal("local capacity overflow opened a cluster-wide watch")
		}
	}
}

func BenchmarkWatchSubscriptionPlanning(b *testing.B) {
	for _, count := range []int{0, 100, 1000} {
		b.Run(fmt.Sprintf("subscriptions=%d", count+3), func(b *testing.B) {
			f := watchFixtureFor(b, count)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := f.watcher.Reconcile(b.Context(), reconcile.Request{}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(f.lists)/float64(b.N), "LIST/op")
		})
	}
}

func BenchmarkWatchEventSkip(b *testing.B) {
	for _, kind := range []string{"managed", "unchanged", "selector-miss"} {
		b.Run(kind, func(b *testing.B) {
			f := watchFixtureFor(b, 100)
			before := sourceObject("a")
			after := before.DeepCopy()
			after.SetResourceVersion("2")
			switch kind {
			case "managed":
				after.SetLabels(map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController})
			case "unchanged":
				after.SetResourceVersion(before.GetResourceVersion())
			case "selector-miss":
				before.SetLabels(nil)
				after.SetLabels(nil)
			}
			b.ReportAllocs()
			for b.Loop() {
				f.watcher.enqueueEvent(b.Context(), configMapsGVR, admissionv1.Update, before, after)
				select {
				case event := <-f.watcher.events:
					f.watcher.matchEvent(b.Context(), event)
				default:
				}
			}
			if len(f.notifications.take()) != 0 {
				b.Fatal("skip path notified a target")
			}
			b.ReportMetric(float64(f.lists)/float64(b.N), "LIST/op")
			b.ReportMetric(float64(f.gets)/float64(b.N), "GET/op")
		})
	}
}
