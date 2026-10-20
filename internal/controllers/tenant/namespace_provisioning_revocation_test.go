// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func revocationBinding(subject string) rbac.AdditionalRoleBindingsSpec {
	return rbac.AdditionalRoleBindingsSpec{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: subject}}}
}

// Inject a persistent profile failure without failing RoleBinding operations.
func failNamespaceProfile(t testing.TB, r *Manager, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace, phase string, failure error) client.WithWatch {
	t.Helper()
	base := r.Client.(client.WithWatch)
	if phase == "rulestatus" {
		require.NoError(t, base.Delete(t.Context(), &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}))
	} else {
		tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{Labels: map[string]string{"managed": "updated"}}}}
	}

	r.Client = interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*capsulev1beta2.RuleStatus); ok && obj.GetNamespace() == ns.Name && phase == "rulestatus" {
				return failure
			}
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*corev1.Namespace); ok && obj.GetName() == ns.Name && phase == "metadata" {
				return failure
			}
			return c.Update(ctx, obj, opts...)
		},
	})
	return base
}

func TestNamespaceProfileFailureRevokesPermissions(t *testing.T) {
	for _, phase := range []string{"rulestatus", "metadata"} {
		for _, source := range []string{"owner", "rule", "promotion", "namespace-selector"} {
			t.Run(phase+"/"+source, func(t *testing.T) {
				ctx := t.Context()
				tnt := provisioningTenant()
				tnt.Spec.AdditionalRoleBindings = []rbac.AdditionalRoleBindingsSpec{revocationBinding("keeper")}
				selected, plain := cleanupOwnedNamespace(tnt, "selected", false), cleanupOwnedNamespace(tnt, "plain", false)
				selected.Labels["profile"] = "selected"
				oldSubject := "old-reader"
				switch source {
				case "owner":
					oldSubject = "alice"
				case "rule", "namespace-selector":
					tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{revocationBinding(oldSubject)}}}}
				case "promotion":
					tnt.Status.Promotions = rbac.PromotionStatusListSpec{{Kind: rbac.UserOwner, Name: oldSubject, ClusterRoles: []string{"view"}, Targets: []string{selected.Name}}}
				}
				other := provisioningTenant()
				other.Name, other.UID = "tenant-b", "tenant-b-uid"
				foreign := cleanupOwnedNamespace(other, "foreign", false)
				r, _ := namespaceCleanupFixture(t, tnt, selected, plain, other, foreign)
				for _, scope := range []struct {
					ns     *corev1.Namespace
					tenant *capsulev1beta2.Tenant
				}{{selected, tnt}, {plain, tnt}, {foreign, other}} {
					plan := prepareNamespaceRoleBindings(scope.tenant)
					_, err := r.provisionNamespace(ctx, logr.Discard(), scope.ns, scope.tenant, &plan)
					require.NoError(t, err)
				}
				assertBindingSubject(t, ctx, r.Client, selected.Name, oldSubject, true)
				switch source {
				case "owner":
					tnt.Spec.Owners[0].Name, tnt.Status.Owners[0].Name = "replacement", "replacement"
				case "rule":
					tnt.Spec.Rules[0].Permissions.Bindings = []rbac.AdditionalRoleBindingsSpec{revocationBinding("replacement")}
				case "promotion":
					tnt.Status.Promotions[0].Name = "replacement"
				case "namespace-selector":
					tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "next"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{revocationBinding("replacement")}}})
					require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(selected), selected))
					selected.Labels["profile"] = "next"
					require.NoError(t, r.Update(ctx, selected))
				}
				failure := errors.New("profile unavailable")
				base := failNamespaceProfile(t, r, tnt, selected, phase, failure)
				plan := prepareNamespaceRoleBindings(tnt)
				input := tnt.DeepCopy()
				var failed *capsulev1beta2.TenantStatusNamespaceItem
				for range 2 {
					stat, err := r.provisionNamespace(ctx, logr.Discard(), selected, tnt, &plan)
					require.ErrorIs(t, err, failure)
					require.Equal(t, metav1.ConditionFalse, stat.Conditions.GetConditionByType(meta.ReadyCondition).Status)
					failed = stat
					assertBindingSubject(t, ctx, base, selected.Name, oldSubject, false)
					assertBindingSubject(t, ctx, base, selected.Name, "keeper", true)
					assertBindingSubject(t, ctx, base, selected.Name, "replacement", false)
				}
				require.Equal(t, input, tnt, "shared evaluated profile must remain immutable")
				if phase == "metadata" {
					// Publish the failed profile exactly as the Tenant queue does,
					// then exercise a subsequent namespace event on the fast queue.
					current := &capsulev1beta2.Tenant{}
					require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(tnt), current))
					current.Spec = *tnt.Spec.DeepCopy()
					require.NoError(t, base.Update(ctx, current))
					require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(tnt), current))
					current.Status.ObservedGeneration = current.Generation
					current.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{failed}
					require.NoError(t, base.Status().Update(ctx, current))
					_, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(selected)})
					require.NoError(t, err)
					assertBindingSubject(t, ctx, base, selected.Name, "replacement", false)
				}
				assertBindingSubject(t, ctx, base, plain.Name, "alice", true)
				assertBindingSubject(t, ctx, base, foreign.Name, "alice", true)
				assertBindingSubject(t, ctx, base, foreign.Name, "keeper", false)
				r.Client = base
				_, err := r.provisionNamespace(ctx, logr.Discard(), selected, tnt, &plan)
				require.NoError(t, err)
				assertBindingSubject(t, ctx, base, selected.Name, "replacement", true)
			})
		}
	}
}

func TestNamespaceProfileFailurePruningErrorsAndIncompletePlans(t *testing.T) {
	for _, mode := range []string{"list-error", "delete-error", "invalid-selector", "non-pruning-plan", "empty-plan"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			r, _ := namespaceCleanupFixture(t, tnt, ns)
			plan := prepareNamespaceRoleBindings(tnt)
			_, err := r.provisionNamespace(ctx, logr.Discard(), ns, tnt, &plan)
			require.NoError(t, err)
			tnt.Spec.Owners, tnt.Status.Owners = nil, nil
			profileErr, pruneErr := errors.New("profile unavailable"), errors.New("RBAC unavailable")
			base := failNamespaceProfile(t, r, tnt, ns, "rulestatus", profileErr)
			plan = prepareNamespaceRoleBindings(tnt)
			if mode == "invalid-selector" {
				plan.rules = []namespaceRoleBindingRule{{selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "profile", Operator: "invalid"}}}}}
			}
			if mode == "non-pruning-plan" {
				plan.prune = false
			}
			lists, deletes := 0, 0
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
					if _, ok := obj.(*rbacv1.RoleBindingList); ok {
						lists++
						if mode == "list-error" {
							return pruneErr
						}
					}
					return c.List(ctx, obj, opts...)
				},
				DeleteAllOf: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
					deletes++
					if mode == "delete-error" {
						return pruneErr
					}
					return c.DeleteAllOf(ctx, obj, opts...)
				},
			})
			_, err = r.provisionNamespace(ctx, logr.Discard(), ns, tnt, &plan)
			require.ErrorIs(t, err, profileErr)
			if mode == "list-error" || mode == "delete-error" {
				require.ErrorIs(t, err, pruneErr)
			}
			if mode == "invalid-selector" {
				require.ErrorContains(t, err, "invalid namespaceSelector")
			}
			if mode == "invalid-selector" || mode == "non-pruning-plan" {
				require.Zero(t, lists)
				require.Zero(t, deletes)
			}
			assertBindingSubject(t, ctx, base, ns.Name, "alice", mode != "empty-plan")
		})
	}
}

func TestNamespaceQueueProfileFailureGate(t *testing.T) {
	for _, mode := range []string{"initial", "profile-failed", "permissions-failed", "old-uid", "ready"} {
		t.Run(mode, func(t *testing.T) {
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			if mode != "initial" {
				ready := meta.NewReadyCondition(ns)
				ready.Status, ready.Reason = metav1.ConditionFalse, meta.FailedReason
				if mode == "ready" {
					ready.Status = metav1.ConditionTrue
				}
				if mode == "permissions-failed" {
					ready.Reason = meta.PermissionsFailedReason
				}
				tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{ready}}}
				if mode == "old-uid" {
					tnt.Status.Spaces[0].UID = "old-namespace"
				}
			}
			r, _ := namespaceCleanupFixture(t, tnt, ns)
			reads, lists, writes := 0, 0, 0
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
					lists++
					return c.List(ctx, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					writes++
					return c.Create(ctx, obj, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					writes++
					return c.Update(ctx, obj, opts...)
				},
			})
			r.provisioningReader = r.Client
			_, err := r.reconcileNamespaceProvisioning(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)})
			require.NoError(t, err)
			if mode == "profile-failed" || mode == "ready" {
				require.Equal(t, 2, reads)
				require.Zero(t, lists)
				require.Zero(t, writes)
			}
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "alice", mode != "profile-failed" && mode != "ready")
		})
	}
}

func BenchmarkNamespaceProfileRevocation(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, bindings := range []int{1, 32} {
			for _, failed := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/bindings=%d/failed=%v", tenants, bindings, failed), func(b *testing.B) {
					tnt := provisioningTenant()
					for i := range bindings {
						tnt.Spec.AdditionalRoleBindings = append(tnt.Spec.AdditionalRoleBindings, revocationBinding(fmt.Sprintf("reader-%d", i)))
					}
					ns := cleanupOwnedNamespace(tnt, "selected", false)
					objects := []client.Object{tnt, ns}
					for i := 1; i < tenants; i++ {
						other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("other-%d", i), UID: types.UID(fmt.Sprintf("other-%d", i))}
						objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
					}
					r, _ := namespaceCleanupFixture(b, objects...)
					r.Client = provisioningDefaultingClient(r.Client.(client.WithWatch))
					plan := prepareNamespaceRoleBindings(tnt)
					_, err := r.provisionNamespace(b.Context(), logr.Discard(), ns, tnt, &plan)
					require.NoError(b, err)
					failure := errors.New("profile unavailable")
					if failed {
						failNamespaceProfile(b, r, tnt, ns, "rulestatus", failure)
					}
					calls := &mockclient.CallCounter{}
					r.Client = interceptor.NewClient(r.Client.(client.WithWatch), calls.Interceptors())
					r.provisioningReader = r.Client
					b.ReportAllocs()
					for b.Loop() {
						_, err := r.provisionNamespace(b.Context(), logr.Discard(), ns, tnt, &plan)
						if failed {
							require.ErrorIs(b, err, failure)
						} else {
							require.NoError(b, err)
						}
					}
					calls.Report(b)
				})
			}
		}
	}
}

func BenchmarkNamespaceProfileFailedPruning(b *testing.B) {
	for _, bindings := range []int{1, 32} {
		b.Run(fmt.Sprintf("bindings=%d", bindings), func(b *testing.B) {
			tnt := provisioningTenant()
			for i := range bindings {
				tnt.Spec.AdditionalRoleBindings = append(tnt.Spec.AdditionalRoleBindings, revocationBinding(fmt.Sprintf("reader-%d", i)))
			}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			r, _ := namespaceCleanupFixture(b, tnt, ns)
			r.Client = provisioningDefaultingClient(r.Client.(client.WithWatch))
			plan := prepareNamespaceRoleBindings(tnt)
			_, err := r.provisionNamespace(b.Context(), logr.Discard(), ns, tnt, &plan)
			require.NoError(b, err)
			failure := errors.New("profile unavailable")
			base := failNamespaceProfile(b, r, tnt, ns, "rulestatus", failure)
			obsolete := &rbacv1.RoleBinding{
				Name: "obsolete", Namespace: ns.Name,
				Labels:   map[string]string{meta.RolebindingLabel: utils.RoleBindingHashFunc(revocationBinding("revoked"))},
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "view"},
				Subjects: revocationBinding("revoked").Subjects,
			}
			calls := &mockclient.CallCounter{}
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), calls.Interceptors())
			r.provisioningReader = r.Client
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				// Restore one obsolete grant outside measurement, including when
				// benchmarking the old implementation that failed to remove it.
				require.NoError(b, client.IgnoreNotFound(base.Delete(b.Context(), obsolete)))
				seed := obsolete.DeepCopy()
				require.NoError(b, base.Create(b.Context(), seed))
				b.StartTimer()
				_, err := r.provisionNamespace(b.Context(), logr.Discard(), ns, tnt, &plan)
				require.ErrorIs(b, err, failure)
			}
			calls.Report(b)
		})
	}
}
