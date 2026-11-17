// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/metrics"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestTriggerReconcileDoesNotRestoreCachedAcknowledgement(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, cordoned := range []bool{false, true} {
			for _, mode := range []string{"enabled", "removed", "state-only"} {
				t.Run(fmt.Sprintf("global=%t/cordoned=%t/mode=%s", global, cordoned, mode), func(t *testing.T) {
					scheme := runtime.NewScheme()
					_ = capsulev1beta2.AddToScheme(scheme)
					_ = corev1.AddToScheme(scheme)
					common := capsulev1beta2.TenantResourceCommonSpec{Cordoned: new(cordoned), PruningOnDelete: new(false), Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}}}
					if !cordoned {
						common.DependsOn = []meta.LocalRFC1123ObjectReference{{Name: "missing"}}
					}
					if mode != "enabled" {
						common.Triggers = nil
					}
					var parent client.Object
					if global {
						parent = &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "source", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
					} else {
						parent = &capsulev1beta2.TenantResource{Name: "source", Namespace: "a", UID: "source", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
					}
					status := func(object client.Object) *capsulev1beta2.TenantResourceCommonStatus {
						if object, ok := object.(*capsulev1beta2.GlobalTenantResource); ok {
							return &object.Status.TenantResourceCommonStatus
						}
						return &object.(*capsulev1beta2.TenantResource).Status.TenantResourceCommonStatus
					}
					parent.SetAnnotations(map[string]string{replication.TriggerAnnotation: "old-request"})
					if mode == "state-only" {
						parent.SetAnnotations(nil)
					}
					recordTrigger(status(parent), "older-request", time.Unix(100, 0))
					c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, &corev1.Namespace{Name: "a"}).WithStatusSubresource(parent).Build()
					if err := c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent); err != nil {
						t.Fatal(err)
					}
					stale := parent.DeepCopyObject().(client.Object)
					parent.SetAnnotations(map[string]string{replication.TriggerAnnotation: "new-request"})
					if mode == "state-only" {
						parent.SetAnnotations(nil)
					}
					if err := c.Update(t.Context(), parent); err != nil {
						t.Fatal(err)
					}
					recordTrigger(status(parent), "new-request", time.Unix(200, 0))
					if err := c.Status().Update(t.Context(), parent); err != nil {
						t.Fatal(err)
					}
					cached := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, inner client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
						if key == client.ObjectKeyFromObject(parent) {
							if global {
								*object.(*capsulev1beta2.GlobalTenantResource) = *stale.(*capsulev1beta2.GlobalTenantResource).DeepCopy()
							} else {
								*object.(*capsulev1beta2.TenantResource) = *stale.(*capsulev1beta2.TenantResource).DeepCopy()
							}
							return nil
						}
						return inner.Get(ctx, key, object, opts...)
					}})
					conditions, _ := cache.NewCELCache()
					var controller reconcile.Reconciler
					if global {
						controller = &globalResourceController{client: cached, reader: c, conditions: conditions, metrics: metrics.NewGlobalTenantResourceRecorder()}
					} else {
						controller = &namespacedResourceController{client: cached, reader: c, conditions: conditions, metrics: metrics.NewTenantResourceRecorder()}
					}
					// Both early error and cordoned status writes must retain the
					// acknowledgement already persisted by the previous attempt.
					if _, err := controller.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)}); err != nil {
						t.Fatal(err)
					}
					if err := c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent); err != nil {
						t.Fatal(err)
					}
					if got := status(parent).Trigger; got == nil || got.ObservedRequest != "new-request" || !got.LastTriggeredAt.Equal(&metav1.MicroTime{Time: time.Unix(200, 0)}) {
						t.Fatalf("cached reconciliation regressed trigger status: %+v", got)
					}
				})
			}
		}
	}
}

func TestTriggerSnapshotReadFailuresDoNotWriteStatus(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, failure := range []error{apierrors.NewServiceUnavailable("API unavailable"), apierrors.NewNotFound(schema.GroupResource{Group: capsulev1beta2.GroupVersion.Group, Resource: "tenantresources"}, "source")} {
			t.Run(fmt.Sprintf("global=%t/error=%s", global, failure), func(t *testing.T) {
				scheme := runtime.NewScheme()
				_ = capsulev1beta2.AddToScheme(scheme)
				common := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}}
				var parent client.Object = &capsulev1beta2.TenantResource{Name: "source", Namespace: "a", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
				if global {
					parent = &capsulev1beta2.GlobalTenantResource{Name: "source", Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
				}
				writes, reads := 0, 0
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).WithStatusSubresource(parent).WithInterceptorFuncs(interceptor.Funcs{SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
					writes++
					return nil
				}, SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
					writes++
					return nil
				}}).Build()
				reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					reads++
					return failure
				}})
				var controller reconcile.Reconciler = &namespacedResourceController{client: c, reader: reader, metrics: metrics.NewTenantResourceRecorder()}
				if global {
					controller = &globalResourceController{client: c, reader: reader, metrics: metrics.NewGlobalTenantResourceRecorder()}
				}
				_, err := controller.Reconcile(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
				if reads != 1 || writes != 0 || (!apierrors.IsNotFound(failure) && !errors.Is(err, failure)) || (apierrors.IsNotFound(failure) && err != nil) {
					t.Fatalf("read failure: reads=%d writes=%d err=%v", reads, writes, err)
				}
			})
		}
	}
}

func BenchmarkControllerTriggerSnapshot(b *testing.B) {
	for _, global := range []bool{false, true} {
		for _, triggered := range []bool{false, true} {
			b.Run(fmt.Sprintf("global=%t/triggered=%t", global, triggered), func(b *testing.B) {
				scheme := runtime.NewScheme()
				_ = capsulev1beta2.AddToScheme(scheme)
				common := capsulev1beta2.TenantResourceCommonSpec{Cordoned: new(true)}
				if triggered {
					common.Triggers = []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}}
				}
				var parent client.Object = &capsulev1beta2.TenantResource{Name: "source", Namespace: "a", UID: "source", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
				if global {
					parent = &capsulev1beta2.GlobalTenantResource{Name: "source", UID: "source", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common}}
				}
				calls := &mockclient.CallCounter{}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).WithStatusSubresource(parent).WithInterceptorFuncs(calls.Interceptors()).Build()
				prepared := cache.NewPreparedReplicationTriggers()
				if triggered {
					compiler, _ := cache.NewCELCache()
					watcher := ResourceWatches{prepared: prepared, conditions: compiler, selectors: cache.NewLabelSelectorCache()}
					if _, err := watcher.prepareSource(parent); err != nil {
						b.Fatal(err)
					}
				}
				var controller reconcile.Reconciler = &namespacedResourceController{client: c, reader: c, triggers: prepared, metrics: metrics.NewTenantResourceRecorder()}
				if global {
					controller = &globalResourceController{client: c, reader: c, triggers: prepared, metrics: metrics.NewGlobalTenantResourceRecorder()}
				}
				request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)}
				if _, err := controller.Reconcile(b.Context(), request); err != nil {
					b.Fatal(err)
				}
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if _, err := controller.Reconcile(b.Context(), request); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
			})
		}
	}
}

func TestReplicationSnapshotReadCounts(t *testing.T) {
	for _, mode := range []string{"legacy", "subscription", "annotation", "retained-status"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = capsulev1beta2.AddToScheme(scheme)
			parent := &capsulev1beta2.TenantResource{Name: "source", Namespace: "a"}
			switch mode {
			case "subscription":
				parent.Spec.Triggers = []capsulev1beta2.TenantResourceTrigger{{Name: "events"}}
			case "annotation":
				parent.Annotations = map[string]string{replication.TriggerAnnotation: "queued"}
			case "retained-status":
				recordTrigger(&parent.Status.TenantResourceCommonStatus, "handled", time.Unix(100, 0))
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			reads := 0
			reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, inner client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
				reads++
				return inner.Get(ctx, key, object, opts...)
			}})
			if err := readReplicationSource(t.Context(), c, reader, client.ObjectKeyFromObject(parent), &capsulev1beta2.TenantResource{}); err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "legacy" {
				want = 0
			}
			if reads != want {
				t.Fatalf("authoritative reads=%d, want %d", reads, want)
			}
		})
	}
}

func TestReplicationSnapshotDoesNotRetainOmittedFields(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprint(global), func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = capsulev1beta2.AddToScheme(scheme)
			var parent client.Object = &capsulev1beta2.TenantResource{Name: "source", Namespace: "a"}
			if global {
				parent = &capsulev1beta2.GlobalTenantResource{Name: "source"}
			}
			parent.SetAnnotations(map[string]string{replication.TriggerAnnotation: "removed", "projectcapsule.dev/keep": "old"})
			triggerSpec(parent).Triggers = []capsulev1beta2.TenantResourceTrigger{{Name: "removed"}}
			var status *capsulev1beta2.TenantResourceCommonStatus
			if global {
				status = &parent.(*capsulev1beta2.GlobalTenantResource).Status.TenantResourceCommonStatus
			} else {
				status = &parent.(*capsulev1beta2.TenantResource).Status.TenantResourceCommonStatus
			}
			recordTrigger(status, "removed", time.Unix(100, 0))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).Build()
			kind := "TenantResource"
			if global {
				kind = "GlobalTenantResource"
			}
			gvk := capsulev1beta2.GroupVersion.WithKind(kind)
			cached := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, inner client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
				if err := inner.Get(ctx, key, object, opts...); err != nil {
					return err
				}
				object.GetObjectKind().SetGroupVersionKind(gvk)
				return nil
			}})
			key := client.ObjectKeyFromObject(parent)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"apiVersion":%q,"kind":%q,"metadata":{"name":%q,"namespace":%q,"annotations":{"projectcapsule.dev/keep":"current"}},"spec":{},"status":{}}`, gvk.GroupVersion().String(), kind, key.Name, key.Namespace)
			}))
			defer server.Close()
			mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{capsulev1beta2.GroupVersion})
			scope := apimeta.RESTScopeNamespace
			if global {
				scope = apimeta.RESTScopeRoot
			}
			mapper.Add(gvk, scope)
			// Exercise the real typed REST decoder: unlike the manager cache,
			// it clears the decoded object's GroupVersionKind.
			reader, err := client.New(&rest.Config{Host: server.URL}, client.Options{Scheme: scheme, Mapper: mapper})
			if err != nil {
				t.Fatal(err)
			}
			if err := readReplicationSource(t.Context(), cached, reader, client.ObjectKeyFromObject(parent), parent); err != nil {
				t.Fatal(err)
			}
			if parent.GetObjectKind().GroupVersionKind() != gvk {
				t.Fatalf("snapshot lost its API identity: %v", parent.GetObjectKind().GroupVersionKind())
			}
			if len(triggerSpec(parent).Triggers) != 0 || status.Trigger != nil || parent.GetAnnotations()[replication.TriggerAnnotation] != "" || parent.GetAnnotations()["projectcapsule.dev/keep"] != "current" {
				t.Fatalf("cached fields survived authoritative read: %+v", parent)
			}
		})
	}
}
