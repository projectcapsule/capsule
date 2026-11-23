// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
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
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	tenantresourceindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func globalProfileEventFixture(t testing.TB) (*globalResourceController, *corev1.Namespace) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	tnt := &capsulev1beta2.Tenant{Name: "a", UID: "uid-a", Labels: map[string]string{"tenant": "a"}}
	ns := &corev1.Namespace{Name: "a-0", Labels: map[string]string{meta.TenantLabel: "a", "profile": "selected", "extra": "selected"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "a", UID: tnt.UID}}}
	source := &capsulev1beta2.GlobalTenantResource{Name: "selected", Spec: capsulev1beta2.GlobalTenantResourceSpec{
		Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}},
		TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{
			{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}},
			{NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "extra", Operator: metav1.LabelSelectorOpIn, Values: []string{"selected"}}}}},
		}},
	}}
	other := source.DeepCopy()
	other.Name, other.Spec.TenantSelector.MatchLabels["tenant"] = "other-tenant", "b"
	health := source.DeepCopy()
	health.Name, health.Spec.HealthChecks = "health", &capsulev1beta2.TenantResourceHealthChecks{Wait: true}
	unselected := source.DeepCopy()
	unselected.Name, unselected.Spec.Resources[0].NamespaceSelector.MatchLabels["profile"] = "unselected", "other"
	unselected.Spec.Resources = unselected.Spec.Resources[:1]
	index := tenantresourceindexer.GlobalHealthNamespaces{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, source, other, health, unselected).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	return &globalResourceController{client: c, log: logr.Discard(), collector: Collector{selectors: cache.NewLabelSelectorCache()}}, ns
}

func TestGlobalNamespaceDeselections(t *testing.T) {
	for _, change := range []string{"all-blocks", "partial-block", "unchanged", "unrelated-label", "addition", "reassignment", "termination", "stale-owner", "missing-tenant", "get-error", "list-error", "invalid-selector"} {
		t.Run(change, func(t *testing.T) {
			r, before := globalProfileEventFixture(t)
			after := before.DeepCopy()
			after.Labels["profile"], after.Labels["extra"] = "excluded", "excluded"
			want := []reconcile.Request{{Name: "selected"}}
			wantGets, wantLists := 1, 1
			switch change {
			case "partial-block":
				after.Labels["profile"] = "selected"
			case "unchanged", "unrelated-label":
				after = before.DeepCopy()
				if change == "unrelated-label" {
					after.Labels["unrelated"] = "changed"
				}
				want = nil
			case "addition":
				before, after = after, before
				want = nil
			case "reassignment":
				after = before.DeepCopy()
				after.Labels[meta.TenantLabel] = "b"
				after.OwnerReferences[0].Name, after.OwnerReferences[0].UID = "b", "uid-b"
			case "termination":
				after = before.DeepCopy()
				after.DeletionTimestamp = new(metav1.Now())
			case "stale-owner", "missing-tenant":
				before.OwnerReferences[0].UID = "previous-a"
				if change == "missing-tenant" {
					before.OwnerReferences[0].Name = "missing"
				}
				want, wantLists = nil, 0
			case "get-error":
				want, wantLists = nil, 0
			case "list-error":
				want = nil
			case "invalid-selector":
				source := &capsulev1beta2.GlobalTenantResource{}
				require.NoError(t, r.client.Get(t.Context(), client.ObjectKey{Name: "selected"}, source))
				source.Spec.Resources = []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "profile", Operator: "invalid"}}}}}
				require.NoError(t, r.client.Update(t.Context(), source))
				want = nil
			}
			gets, lists := 0, 0
			r.client = interceptor.NewClient(r.client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					gets++
					if change == "get-error" {
						return errors.New("cache unavailable")
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					lists++
					options := (&client.ListOptions{}).ApplyOptions(opts)
					require.Equal(t, tenantresourceindexer.GlobalHealthNamespacesFieldName+"=profile", options.FieldSelector.String())
					if change == "list-error" {
						return errors.New("cache unavailable")
					}
					return c.List(ctx, list, opts...)
				},
			})
			originalBefore, originalAfter := before.DeepCopy(), after.DeepCopy()
			require.Equal(t, want, r.enqueueNamespaceDeselections(t.Context(), before, after))
			require.Equal(t, wantGets, gets)
			require.Equal(t, wantLists, lists)
			require.Equal(t, originalBefore, before)
			require.Equal(t, originalAfter, after)
		})
	}
}

func TestGlobalNamespaceHandlerPreservesHealthAndPruning(t *testing.T) {
	r, before := globalProfileEventFixture(t)
	after := before.DeepCopy()
	delete(after.Labels, "extra")
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	h := r.globalNamespaceHandler()
	h.Update(t.Context(), event.UpdateEvent{ObjectOld: before, ObjectNew: after}, queue)
	h.Update(t.Context(), event.UpdateEvent{}, queue)
	var requests []reconcile.Request
	for queue.Len() > 0 {
		request, _ := queue.Get()
		requests = append(requests, request)
		queue.Done(request)
	}
	require.ElementsMatch(t, []reconcile.Request{{Name: "selected"}, {Name: "health"}}, requests)
}

func BenchmarkGlobalNamespaceDeselection(b *testing.B) {
	for _, path := range []string{"addition", "deselection"} {
		for _, state := range []string{"cold", "warm", "concurrent"} {
			b.Run(path+"/"+state, func(b *testing.B) {
				r, before := globalProfileEventFixture(b)
				after := before.DeepCopy()
				delete(after.Labels, "extra")
				want := 1
				if path == "addition" {
					before, after, want = after, before, 0
				}
				b.ReportAllocs()
				if state != "cold" {
					r.enqueueNamespaceDeselections(b.Context(), before, after)
				}
				if state == "concurrent" {
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if got := r.enqueueNamespaceDeselections(b.Context(), before, after); len(got) != want {
								b.Errorf("unexpected requests: %v", got)
								return
							}
						}
					})
					return
				}
				for b.Loop() {
					if state == "cold" {
						r.collector.selectors = cache.NewLabelSelectorCache()
					}
					if got := r.enqueueNamespaceDeselections(b.Context(), before, after); len(got) != want {
						b.Fatalf("unexpected requests: %v", got)
					}
				}
			})
		}
	}
}

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
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
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
				want = want[:1]
				wantGets = 2
			case "delete":
				h.Delete(t.Context(), event.DeleteEvent{Object: ns}, queue)
			case "profile":
				after.Labels["profile"] = "selected"
				h.Update(t.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
				wantLists, wantGets = 2, 1
			case "termination":
				after.DeletionTimestamp = new(metav1.Now())
				h.Update(t.Context(), event.UpdateEvent{ObjectOld: ns, ObjectNew: after}, queue)
			case "owner-only", "stale-owner":
				after.Labels = nil
				if change == "stale-owner" {
					after.OwnerReferences[0].UID = "previous-tenant"
				}
				h.Create(t.Context(), event.CreateEvent{Object: after}, queue)
				wantLists, wantGets = 2, 1
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
						r.enqueueTenantResourcesForNamespaceProfile(b.Context(), ns)
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
