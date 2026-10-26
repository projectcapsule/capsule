// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestResourcePoliciesConvergeAfterPermissionFailures(t *testing.T) {
	for _, phase := range []string{"bindings", "pruning", "profile"} {
		t.Run(phase, func(t *testing.T) {
			tnt := provisioningTenant()
			tnt.Spec.NetworkPolicies.Items = []networkingv1.NetworkPolicySpec{{PodSelector: metav1.LabelSelector{}}}
			tnt.Spec.LimitRanges.Items = []corev1.LimitRangeSpec{{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}}}}}
			tnt.Spec.ResourceQuota = api.ResourceQuotaSpec{Scope: api.ResourceQuotaScopeNamespace, Items: []corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{corev1.ResourceConfigMaps: resource.MustParse("10")}}}}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
			base := r.Client
			failure := errors.New("injected " + phase + " failure")
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					_, binding := obj.(*rbacv1.RoleBinding)
					_, profile := obj.(*capsulev1beta2.RuleStatus)
					if (binding && phase == "bindings") || (profile && phase == "profile") {
						return failure
					}
					return c.Create(ctx, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
					if _, ok := obj.(*rbacv1.RoleBindingList); ok && phase == "pruning" {
						return failure
					}
					return c.List(ctx, obj, opts...)
				},
			})
			snapshot := tnt.DeepCopy()
			if err := r.reconcile(t.Context(), logr.Discard(), snapshot); !errors.Is(err, failure) {
				t.Fatalf("lost reconciliation failure: %v", err)
			}
			status := snapshot.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name})
			ready := status.Conditions.GetConditionByType(meta.ReadyCondition)
			wantReason := meta.PermissionsFailedReason
			wantResources := 1
			if phase == "profile" {
				wantReason, wantResources = meta.FailedReason, 0
			}
			if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != wantReason {
				t.Fatalf("wrong failure condition: %+v", ready)
			}
			policies, limits, quotas := &networkingv1.NetworkPolicyList{}, &corev1.LimitRangeList{}, &corev1.ResourceQuotaList{}
			for _, list := range []client.ObjectList{policies, limits, quotas} {
				if err := base.List(t.Context(), list, client.InNamespace(ns.Name)); err != nil {
					t.Fatal(err)
				}
			}
			if len(policies.Items) != wantResources || len(limits.Items) != wantResources || len(quotas.Items) != wantResources {
				t.Fatalf("resource phases blocked by %s: policies=%d limits=%d quotas=%d", phase, len(policies.Items), len(limits.Items), len(quotas.Items))
			}
			assertBindingSubject(t, t.Context(), base, foreign.Name, "alice", false)
			foreignPolicies := &networkingv1.NetworkPolicyList{}
			if err := base.List(t.Context(), foreignPolicies, client.InNamespace(foreign.Name)); err != nil {
				t.Fatal(err)
			}
			if len(foreignPolicies.Items) != 0 {
				t.Fatal("Tenant A policies leaked to Tenant B")
			}
			r.Client = base
			if err := r.reconcile(t.Context(), logr.Discard(), snapshot); err != nil {
				t.Fatal(err)
			}
			ready = snapshot.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name}).Conditions.GetConditionByType(meta.ReadyCondition)
			if ready == nil || ready.Status != metav1.ConditionTrue {
				t.Fatalf("permissions did not recover: %+v", ready)
			}
			assertBindingSubject(t, t.Context(), base, ns.Name, "alice", true)
		})
	}
}

func TestNamespacePermissionsReuseTheProfileNamespace(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{
		Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}},
			Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{
				ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: "User", Name: "reader"}},
			}}},
		}},
	}}
	ns := cleanupOwnedNamespace(tnt, "selected", false)
	ns.Labels["profile"] = "reader"
	r, _ := namespaceCleanupFixture(t, tnt, ns)
	var reads atomic.Int32
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				reads.Add(1)
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	if err := r.reconcileActiveTenantNamespaces(t.Context(), logr.Discard(), tnt); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatalf("expected only the metadata update's Namespace GET, got %d", reads.Load())
	}
	assertBindingSubject(t, t.Context(), r.Client, ns.Name, "reader", true)
}

func TestNamespaceProvisioningBoundsWorkersAndHonorsCancellation(t *testing.T) {
	for _, phase := range []string{"profile", "bindings"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Status: capsulev1beta2.TenantStatus{
				Owners: rbac.OwnerStatusListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
			}}
			objects := []client.Object{tnt}
			for i := range 16 {
				objects = append(objects, cleanupOwnedNamespace(tnt, fmt.Sprintf("team-%d", i), false))
			}
			r, _ := namespaceCleanupFixture(t, objects...)
			base := r.Client
			var active, maximum atomic.Int32
			started := make(chan struct{}, 16)
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					_, profile := obj.(*capsulev1beta2.RuleStatus)
					_, binding := obj.(*rbacv1.RoleBinding)
					if (phase == "profile" && profile) || (phase == "bindings" && binding) {
						current := active.Add(1)
						defer active.Add(-1)
						for previous := maximum.Load(); current > previous; previous = maximum.Load() {
							if maximum.CompareAndSwap(previous, current) {
								break
							}
						}
						started <- struct{}{}
						<-ctx.Done()
						return ctx.Err()
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			done := make(chan error, 1)
			go func() { done <- r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), tnt) }()
			for range 8 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Error("namespace workers did not fill the bounded pool")
				}
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("namespace cancellation was lost: %v", err)
			}
			if maximum.Load() != 8 || active.Load() != 0 {
				t.Fatalf("unexpected worker concurrency: maximum=%d active=%d", maximum.Load(), active.Load())
			}
			for _, obj := range objects[1:] {
				assertBindingSubject(t, t.Context(), base, obj.GetName(), "alice", false)
			}
		})
	}
}

func TestNamespaceRoleBindingsDoNotWaitForAnotherNamespace(t *testing.T) {
	for _, failSlow := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed_sibling=%t", failSlow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
			}}
			fast, slow := cleanupOwnedNamespace(tnt, "fast", false), cleanupOwnedNamespace(tnt, "slow", false)
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, fast, slow, other, foreign)
			base := r.Client
			blocked, release, granted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var blockOnce, grantOnce sync.Once
			slowErr := errors.New("slow namespace profile failed")
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*capsulev1beta2.RuleStatus); ok && obj.GetNamespace() == slow.Name {
						blockOnce.Do(func() { close(blocked) })
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
						if failSlow {
							return slowErr
						}
					}
					if err := c.Create(ctx, obj, opts...); err != nil {
						return err
					}
					if _, ok := obj.(*rbacv1.RoleBinding); ok && obj.GetNamespace() == fast.Name {
						grantOnce.Do(func() { close(granted) })
					}
					return nil
				},
			})
			done := make(chan error, 1)
			go func() { done <- r.reconcile(ctx, logr.Discard(), tnt.DeepCopy()) }()
			defer func() {
				close(release)
				err := <-done
				if failSlow {
					if !errors.Is(err, slowErr) {
						t.Errorf("namespace error was lost: %v", err)
					}
					assertBindingSubject(t, ctx, base, slow.Name, "alice", false)
				} else if err != nil {
					t.Errorf("namespace provisioning failed: %v", err)
				} else {
					assertBindingSubject(t, ctx, base, slow.Name, "alice", true)
				}
			}()
			for _, ready := range []<-chan struct{}{blocked, granted} {
				select {
				case <-ready:
				case <-ctx.Done():
					t.Fatal("fast namespace RBAC waited for an unrelated namespace profile")
				}
			}
			status := &capsulev1beta2.RuleStatus{}
			if err := base.Get(ctx, client.ObjectKey{Namespace: fast.Name, Name: meta.NameForManagedRuleStatus()}, status); err != nil {
				t.Fatalf("RBAC preceded its own namespace profile: %v", err)
			}
			assertBindingSubject(t, ctx, base, fast.Name, "alice", true)
			assertBindingSubject(t, ctx, base, slow.Name, "alice", false)
			assertBindingSubject(t, ctx, base, foreign.Name, "alice", false)
		})
	}
}

func TestRoleBindingsPrecedeResourceProvisioning(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{
		Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
	}}
	ns := cleanupOwnedNamespace(tnt, "new", false)
	other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
	foreign := cleanupOwnedNamespace(other, "foreign", false)
	r, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
	base := r.Client
	checked := false
	r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*networkingv1.NetworkPolicyList); ok {
				checked = true
				status := &capsulev1beta2.RuleStatus{}
				if err := base.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status); err != nil {
					return fmt.Errorf("RuleStatus must precede resource provisioning: %w", err)
				}
				assertBindingSubject(t, ctx, base, ns.Name, "alice", true)
				assertBindingSubject(t, ctx, base, foreign.Name, "alice", false)
			}
			return c.List(ctx, list, opts...)
		},
	})
	if err := r.reconcile(t.Context(), logr.Discard(), tnt.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("resource provisioning was skipped")
	}
}

func TestResourceProvisioningRunsConcurrentlyAndJoinsErrors(t *testing.T) {
	for _, cancelWork := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelWork), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Annotations: map[string]string{
				capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10",
			}}
			r, dyn := namespaceCleanupFixture(t, tnt, cleanupOwnedNamespace(tnt, "new", false))
			var finished atomic.Int32
			var recounted, resourceQuotas, globalQuotas atomic.Bool
			dyn.PrependReactor("list", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
				recounted.Store(finished.Load() == 2)
				return true, &unstructured.UnstructuredList{}, nil
			})
			started := make(chan string, 2)
			release := make(chan struct{})
			networkErr, limitsErr := errors.New("network unavailable"), errors.New("limits unavailable")
			var networkOnce, limitsOnce sync.Once
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					var phaseErr error
					switch list.(type) {
					case *networkingv1.NetworkPolicyList:
						networkOnce.Do(func() { started <- "network" })
						phaseErr = networkErr
					case *corev1.LimitRangeList:
						limitsOnce.Do(func() { started <- "limits" })
						phaseErr = limitsErr
					case *corev1.ResourceQuotaList:
						resourceQuotas.Store(true)
						return c.List(ctx, list, opts...)
					case *capsulev1beta2.GlobalResourceQuotaList:
						globalQuotas.Store(true)
						return c.List(ctx, list, opts...)
					default:
						return c.List(ctx, list, opts...)
					}
					defer finished.Add(1)
					select {
					case <-release:
						if err := ctx.Err(); err != nil {
							return err
						}

						return phaseErr
					case <-ctx.Done():
						return ctx.Err()
					}
				},
			})
			r.reader = r.Client
			done := make(chan error, 1)
			go func() { done <- r.reconcile(ctx, logr.Discard(), tnt.DeepCopy()) }()
			defer func() {
				close(release)
				err := <-done
				if cancelWork {
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation was lost: %v", err)
					}
				} else if !errors.Is(err, networkErr) || !errors.Is(err, limitsErr) {
					t.Errorf("independent phase errors were lost: %v", err)
				}
				if !cancelWork && (!recounted.Load() || !resourceQuotas.Load() || !globalQuotas.Load()) {
					t.Error("independent quota phases were skipped or usage recount preceded resource provisioning")
				}
			}()
			for range 2 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("resource phases did not start concurrently")
				}
			}
			if cancelWork {
				cancel()
			}
		})
	}
}
