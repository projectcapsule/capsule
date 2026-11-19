// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	tenantresourceindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func namespaceEventFixture(t testing.TB, namespaces int) (*namespacedResourceController, *corev1.Namespace) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	var objects []client.Object
	var target *corev1.Namespace
	for _, tenant := range []string{"a", "b"} {
		tnt := &capsulev1beta2.Tenant{Name: tenant, UID: "uid-" + types.UID(tenant)}
		for i := range namespaces {
			ns := &corev1.Namespace{Name: fmt.Sprintf("%s-%d", tenant, i), Labels: map[string]string{meta.TenantLabel: tenant}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenant, UID: tnt.UID}}}
			objects = append(objects, ns)
			tnt.Status.Namespaces = append(tnt.Status.Namespaces, ns.Name)
			if tenant == "a" && i == 0 {
				target = ns
			}
		}
		objects = append(objects, tnt,
			&capsulev1beta2.TenantResource{Name: "health", Namespace: tenant + "-0", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: healthSpec()}},
			&capsulev1beta2.TenantResource{Name: "legacy", Namespace: tenant + "-0"})
	}
	index := tenantresourceindexer.NamespacedHealthNamespaces{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	return &namespacedResourceController{client: c, log: logr.Discard()}, target
}

func TestNamespaceEventsDispatchOnceAndPreserveScope(t *testing.T) {
	for _, change := range []string{"startup", "create", "delete", "profile", "termination", "owner-only", "stale-owner", "reassignment"} {
		t.Run(change, func(t *testing.T) {
			r, ns := namespaceEventFixture(t, 2)
			lists, gets := 0, 0
			r.client = interceptor.NewClient(r.client.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					lists++
					return c.List(ctx, list, opts...)
				},
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					gets++
					return c.Get(ctx, key, obj, opts...)
				},
			})
			h := r.tenantNamespaceHandler()
			queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer queue.ShutDown()
			wantLists, wantGets := 3, 0
			want := []reconcile.Request{{Namespace: "a-0", Name: "health"}, {Namespace: "a-0", Name: "legacy"}}
			after := ns.DeepCopy()
			switch change {
			case "startup":
				e := event.CreateEvent{Object: ns, IsInInitialList: true}
				if healthNamespaceEvents().Create(e) {
					h.Create(t.Context(), e, queue)
				}
				wantLists, wantGets, want = 0, 0, nil
			case "create":
				h.Create(t.Context(), event.CreateEvent{Object: ns}, queue)
			case "delete":
				h.Delete(t.Context(), event.DeleteEvent{Object: ns}, queue)
			case "profile":
				after.Labels["profile"] = "selected"
				h.Update(t.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
				wantLists, wantGets, want = 2, 1, want[:1]
			case "termination":
				after.DeletionTimestamp = new(metav1.Now())
				h.Update(t.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
			case "owner-only", "stale-owner":
				after.Labels = nil
				if change == "stale-owner" {
					after.OwnerReferences[0].UID = "previous-tenant"
				}
				h.Create(t.Context(), event.CreateEvent{Object: after}, queue)
				wantLists, wantGets, want = 2, 1, want[:1]
				if change == "stale-owner" {
					wantLists, want = 0, nil
				}
			case "reassignment":
				after.Labels[meta.TenantLabel] = "b"
				after.OwnerReferences[0].Name, after.OwnerReferences[0].UID = "b", "uid-b"
				h.Update(t.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
				wantLists = 6
				want = append(want, reconcile.Request{Namespace: "b-0", Name: "health"}, reconcile.Request{Namespace: "b-0", Name: "legacy"})
			}
			var requests []reconcile.Request
			for queue.Len() > 0 {
				request, _ := queue.Get()
				requests = append(requests, request)
				queue.Done(request)
			}
			require.ElementsMatch(t, want, requests)
			require.Equal(t, wantLists, lists)
			require.Equal(t, wantGets, gets)
		})
	}
}

func BenchmarkReplicationNamespaceEvents(b *testing.B) {
	for _, namespaces := range []int{1, 64} {
		for _, path := range []string{"previous-startup", "startup", "profile"} {
			b.Run(fmt.Sprintf("%s/namespaces=%d", path, namespaces), func(b *testing.B) {
				r, ns := namespaceEventFixture(b, namespaces)
				h := r.tenantNamespaceHandler()
				queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
				defer queue.ShutDown()
				initial := event.CreateEvent{Object: ns, IsInInitialList: true}
				after := ns.DeepCopy()
				after.Labels["profile"] = "selected"
				p := healthNamespaceEvents()
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					switch path {
					case "previous-startup":
						r.enqueueTenantResourcesForNamespace(b.Context(), ns)
						r.enqueueHealthTenantResourcesForNamespace(b.Context(), ns)
					case "startup":
						if p.Create(initial) {
							h.Create(b.Context(), initial, queue)
						}
					case "profile":
						h.Update(b.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
					}
				}
			})
		}
	}
}
