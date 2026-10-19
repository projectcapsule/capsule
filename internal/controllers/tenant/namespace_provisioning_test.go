// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestNamespaceQueueDefersPromotionsAndPreservesExistingGrants(t *testing.T) {
	for _, mode := range []string{"recreated", "relabeled", "existing-grant"} {
		t.Run(mode, func(t *testing.T) {
			tnt := provisioningTenant()
			source := cleanupOwnedNamespace(tnt, "source", false)
			source.Labels["profile"] = "privileged"
			ns := cleanupOwnedNamespace(tnt, "target", false)
			ns.Labels["profile"] = "plain"
			if mode == "existing-grant" {
				ns.Labels["profile"] = "privileged"
			}
			tnt.Status.Namespaces = []string{source.Name, ns.Name}
			status := &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionFalse, Reason: meta.PermissionsFailedReason}}}
			if mode == "recreated" {
				status.UID = "previous-namespace-uid"
				status.Conditions = meta.ConditionList{meta.NewReadyCondition(ns)}
			}
			tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{status}
			tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "privileged"}},
				Permissions:       rules.NamespaceRulePermissionBody{Promotions: []*rules.NamespaceRulePromotionRule{{ClusterRoles: []string{"view"}}}},
			}}
			promotion := rbac.PromotionSpec{ClusterRoles: []string{"view"}, Targets: []string{ns.Name}}
			promotion.Kind, promotion.Name = rbac.ServiceAccountOwner, "system:serviceaccount:source:promoted"
			tnt.Status.Promotions = rbac.PromotionStatusListSpec{promotion}
			sa := &corev1.ServiceAccount{Name: "promoted", Namespace: source.Name, Labels: map[string]string{meta.ServiceAccountPromotionLabel: meta.ValueTrue}}
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, source, ns, sa, other, foreign)
			if mode == "existing-grant" {
				plan := prepareNamespaceRoleBindings(tnt)
				if _, err := r.provisionNamespace(t.Context(), logr.Discard(), ns, tnt, &plan); err != nil {
					t.Fatal(err)
				}
				assertBindingSubject(t, t.Context(), r.Client, ns.Name, "promoted", true)
			}
			if _, err := r.reconcileNamespaceProvisioning(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}); err != nil {
				t.Fatal(err)
			}
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "alice", true)
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "promoted", mode == "existing-grant")
			assertBindingSubject(t, t.Context(), r.Client, foreign.Name, "promoted", false)
			// The Tenant queue recomputes selectors, preserves valid grants and
			// records the current namespace UID after recreation.
			snapshot := tnt.DeepCopy()
			if err := r.reconcile(t.Context(), logr.Discard(), snapshot); err != nil {
				t.Fatal(err)
			}
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "promoted", mode == "existing-grant")
			assertBindingSubject(t, t.Context(), r.Client, source.Name, "promoted", true)
			if got := snapshot.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name}); got == nil || got.UID != ns.UID {
				t.Fatalf("namespace recreation retained stale UID: %+v", got)
			}
		})
	}
}

func TestNamespaceQueueProvisionsWhileTenantResourceWorkIsBlocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tnt := provisioningTenant()
	r, _ := namespaceCleanupFixture(t, tnt)
	base := r.Client
	blocked, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var blocking atomic.Bool
	r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*networkingv1.NetworkPolicyList); ok && blocking.CompareAndSwap(false, true) {
				close(blocked)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return c.List(ctx, list, opts...)
		},
	})
	done := make(chan error, 1)
	go func() { done <- r.reconcile(ctx, logr.Discard(), tnt.DeepCopy()) }()
	defer func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	}()
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("Tenant resource phase did not start")
	}
	ns := cleanupOwnedNamespace(tnt, "created-during-resource-work", false)
	if err := base.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}); err != nil {
		t.Fatal(err)
	}
	assertBindingSubject(t, ctx, base, ns.Name, "alice", true)
	if err := base.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, &capsulev1beta2.RuleStatus{}); err != nil {
		t.Fatal(err)
	}
	// An older Tenant membership snapshot must not garbage collect the new grant.
	for _, obj := range []client.Object{&rbacv1.RoleBinding{}, &capsulev1beta2.RuleStatus{}} {
		if err := r.runGarbageCollection(ctx, tnt, obj); err != nil {
			t.Fatal(err)
		}
	}
	assertBindingSubject(t, ctx, base, ns.Name, "alice", true)
}

func TestNamespaceProvisioningQueueOwnershipAndReadiness(t *testing.T) {
	for _, mode := range []string{"active", "terminating", "foreign-uid", "multiple-owners", "unowned", "missing-namespace", "missing-tenant", "unobserved", "existing-profile", "ready-namespace", "read-error"} {
		t.Run(mode, func(t *testing.T) {
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "new", mode == "terminating")
			objects := []client.Object{tnt, ns}
			switch mode {
			case "foreign-uid":
				ns.OwnerReferences[0].UID = "old-tenant"
			case "multiple-owners":
				ns.OwnerReferences = append(ns.OwnerReferences, ns.OwnerReferences[0])
			case "unowned":
				ns.OwnerReferences = nil
			case "missing-namespace":
				objects = []client.Object{tnt}
			case "missing-tenant":
				objects = []client.Object{ns}
			case "unobserved":
				tnt.Status.ObservedGeneration = 0
			case "ready-namespace":
				tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{meta.NewReadyCondition(ns)}}}
			case "existing-profile":
				objects = append(objects, &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name})
			}
			r, _ := namespaceCleanupFixture(t, objects...)
			failure := errors.New("read failed")
			if mode == "read-error" {
				r.reader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return failure
				}})
			}
			result, err := r.reconcileNamespaceProvisioning(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)})
			if mode == "read-error" {
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (result.RequeueAfter > 0) != (mode == "unobserved") {
				t.Fatalf("unexpected retry: %+v", result)
			}
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "alice", mode == "active" || mode == "existing-profile")
		})
	}
}

func TestNamespaceQueueSkipsBusyKeysAndRefreshesSpecOwnersOnRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tnt := provisioningTenant()
	slow, fast := cleanupOwnedNamespace(tnt, "busy", false), cleanupOwnedNamespace(tnt, "other", false)
	r, _ := namespaceCleanupFixture(t, tnt, slow, fast)
	entered, release, held := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		held <- r.withNamespaceProvisioning(ctx, slow.Name, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	result, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(slow)})
	if err != nil || result.RequeueAfter <= 0 {
		t.Fatalf("busy namespace blocked the queue or was dropped: %+v, %v", result, err)
	}
	if _, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(fast)}); err != nil {
		t.Fatal(err)
	}
	assertBindingSubject(t, ctx, r.Client, fast.Name, "alice", true)
	latest := &capsulev1beta2.Tenant{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(tnt), latest); err != nil {
		t.Fatal(err)
	}
	latest.Spec.Owners = rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "bob", ClusterRoles: []string{"view"}}}
	latest.Generation++
	if err := r.Update(ctx, latest); err != nil {
		t.Fatal(err)
	}
	latest.Status.ObservedGeneration = latest.Generation
	latest.Status.Owners = latest.Spec.Owners.ToStatusOwners()
	if err := r.Status().Update(ctx, latest); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if _, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(slow)}); err != nil {
		t.Fatal(err)
	}
	assertBindingSubject(t, ctx, r.Client, slow.Name, "alice", false)
	assertBindingSubject(t, ctx, r.Client, slow.Name, "bob", true)
}

func TestNamespaceQueueRetriesPartialProvisioning(t *testing.T) {
	for _, phase := range []string{"profile", "bindings"} {
		t.Run(phase, func(t *testing.T) {
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "new", false)
			r, _ := namespaceCleanupFixture(t, tnt, ns)
			base := r.Client
			failure := errors.New("temporary write failure")
			failed := false
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				_, profile := obj.(*capsulev1beta2.RuleStatus)
				_, binding := obj.(*rbacv1.RoleBinding)
				if !failed && ((phase == "profile" && profile) || (phase == "bindings" && binding)) {
					failed = true
					return failure
				}
				return c.Create(ctx, obj, opts...)
			}})
			plan := prepareNamespaceRoleBindings(tnt)
			stat, err := r.provisionNamespace(t.Context(), logr.Discard(), ns, tnt, &plan)
			if !errors.Is(err, failure) {
				t.Fatalf("write failure was lost: %v", err)
			}
			if ready := stat.Conditions.GetConditionByType(meta.ReadyCondition); ready == nil || ready.Status != "False" {
				t.Fatalf("failed provisioning reported ready: %+v", stat)
			}
			assertBindingSubject(t, t.Context(), base, ns.Name, "alice", false)
			// Retry even if the first pass already created the RuleStatus.
			if _, err := r.reconcileNamespaceProvisioning(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}); err != nil {
				t.Fatal(err)
			}
			assertBindingSubject(t, t.Context(), base, ns.Name, "alice", true)
		})
	}
}

func TestNamespaceProvisioningLockCancellationIsolationAndPruning(t *testing.T) {
	r := &Manager{}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- r.withNamespaceProvisioning(ctx, "a", func() error { close(entered); <-release; return nil })
	}()
	<-entered
	waitCtx, stop := context.WithCancel(ctx)
	stop()
	if err := r.withNamespaceProvisioning(waitCtx, "a", func() error { t.Error("cancelled waiter ran"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := r.withNamespaceProvisioning(ctx, "b", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(r.namespaceProvisioning) != 0 {
		t.Fatal("idle namespace keys retained")
	}
}

func TestNamespaceProvisioningUsesIsolatedReaderAndRejectsReusedUID(t *testing.T) {
	tnt := provisioningTenant()
	old := cleanupOwnedNamespace(tnt, "recreated", false)
	current := old.DeepCopy()
	current.UID = "replacement-uid"
	r, _ := namespaceCleanupFixture(t, tnt, current)
	r.provisioningReader = r.Client
	r.reader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		return errors.New("admission reader must not be used")
	}})
	plan := prepareNamespaceRoleBindings(tnt)
	if _, err := r.provisionNamespace(t.Context(), logr.Discard(), old, tnt, &plan); err != nil {
		t.Fatal(err)
	}
	assertBindingSubject(t, t.Context(), r.Client, current.Name, "alice", false)
	if _, err := r.reconcileNamespaceProvisioning(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(current)}); err != nil {
		t.Fatal(err)
	}
	assertBindingSubject(t, t.Context(), r.Client, current.Name, "alice", true)
}

func BenchmarkNamespaceProvisioningQueue(b *testing.B) {
	for _, unrelated := range []int{0, 64} {
		for _, state := range []string{"initial", "ready"} {
			b.Run(fmt.Sprintf("unrelatedTenants=%d/%s", unrelated, state), func(b *testing.B) {
				tnt := provisioningTenant()
				ns := cleanupOwnedNamespace(tnt, "new", false)
				objects := []client.Object{tnt, ns}
				for i := range unrelated {
					objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i)})
				}
				r, _ := namespaceCleanupFixture(b, objects...)
				base := r.Client
				calls := &mockclient.CallCounter{}
				c := interceptor.NewClient(base.(client.WithWatch), calls.Interceptors())
				r.Client, r.reader = c, c
				if state == "ready" {
					current := &capsulev1beta2.Tenant{}
					if err := base.Get(b.Context(), client.ObjectKeyFromObject(tnt), current); err != nil {
						b.Fatal(err)
					}
					current.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{meta.NewReadyCondition(ns)}}}
					if err := base.Status().Update(b.Context(), current); err != nil {
						b.Fatal(err)
					}
				}
				request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}
				if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
					b.Fatal(err)
				}
				calls.Reset()
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if state == "initial" {
						b.StopTimer()
						if err := base.DeleteAllOf(b.Context(), &capsulev1beta2.RuleStatus{}, client.InNamespace(ns.Name)); err != nil {
							b.Fatal(err)
						}
						if err := base.DeleteAllOf(b.Context(), &rbacv1.RoleBinding{}, client.InNamespace(ns.Name)); err != nil {
							b.Fatal(err)
						}
						b.StartTimer()
					}
					if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
			})
		}
	}
}

func BenchmarkNamespaceProvisioningParallel(b *testing.B) {
	for _, keys := range []int{1, 16} {
		b.Run(fmt.Sprintf("namespaces=%d", keys), func(b *testing.B) {
			tnt := provisioningTenant()
			objects := []client.Object{tnt}
			requests := make([]reconcile.Request, keys)
			for i := range keys {
				ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("namespace-%d", i), false)
				objects = append(objects, ns)
				requests[i] = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}
			}
			r, _ := namespaceCleanupFixture(b, objects...)
			calls := &mockclient.CallCounter{}
			c := interceptor.NewClient(r.Client.(client.WithWatch), calls.Interceptors())
			r.Client, r.reader = c, c
			for _, request := range requests {
				if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
					b.Fatal(err)
				}
			}
			// Profiles exist, while Tenant status publication is still pending.
			// Measure real provisioning work with one contended or independent keys.
			calls.Reset()
			var next atomic.Uint64
			var deferred atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					request := requests[next.Add(1)%uint64(keys)]
					result, err := r.reconcileNamespaceProvisioning(b.Context(), request)
					if err != nil {
						b.Error(err)
						return
					}
					if result.RequeueAfter > 0 {
						deferred.Add(1)
					}
				}
			})
			calls.Report(b)
			b.ReportMetric(float64(deferred.Load())/float64(b.N), "deferred/op")
		})
	}
}
