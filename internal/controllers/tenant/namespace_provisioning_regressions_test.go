// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

type provisioningOwnerConfiguration struct {
	cleanupTestConfiguration
	administrators rbac.UserListSpec
}

func (provisioningOwnerConfiguration) AllowServiceAccountPromotion() bool  { return true }
func (c provisioningOwnerConfiguration) Administrators() rbac.UserListSpec { return c.administrators }

func (provisioningOwnerConfiguration) RBAC() *capsulev1beta2.RBACConfiguration {
	return &capsulev1beta2.RBACConfiguration{PromotionClusterRoles: []string{"view"}, AdministrationClusterRoles: []string{"view"}}
}

func TestNamespaceQueueDefersDynamicOwnersUntilEvaluation(t *testing.T) {
	for _, kind := range []string{"tenantowner-delete", "tenantowner-selector", "serviceaccount", "administrator"} {
		for _, revoked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/revoked=%v", kind, revoked), func(t *testing.T) {
				ctx := t.Context()
				tnt := provisioningTenant()
				tnt.Spec.Permissions.AllowOwnerPromotion = true
				tnt.Spec.Permissions.MatchOwners = []*metav1.LabelSelector{{MatchLabels: map[string]string{"owner": "selected"}}}
				source, ns := cleanupOwnedNamespace(tnt, "source", false), cleanupOwnedNamespace(tnt, "new", false)
				tnt.Status.Namespaces = []string{source.Name}
				other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
				foreign := cleanupOwnedNamespace(other, "foreign", false)
				name := "dynamic-user"
				subject := name
				configuration := provisioningOwnerConfiguration{}
				objects := []client.Object{tnt, source, ns, other, foreign}
				var dynamic client.Object
				switch kind {
				case "tenantowner-delete", "tenantowner-selector":
					dynamic = &capsulev1beta2.TenantOwner{Name: "dynamic", Labels: map[string]string{"owner": "selected"}, Spec: capsulev1beta2.TenantOwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: name}, ClusterRoles: []string{"view"}}}}
				case "serviceaccount":
					name = "system:serviceaccount:source:dynamic"
					subject = "dynamic"
					dynamic = &corev1.ServiceAccount{Name: "dynamic", Namespace: source.Name, Labels: map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue}}
				case "administrator":
					configuration.administrators = rbac.UserListSpec{{Kind: rbac.UserOwner, Name: name}}
				}
				if dynamic != nil {
					objects = append(objects, dynamic)
				}
				r, _ := namespaceCleanupFixture(t, objects...)
				r.Configuration = configuration
				require.NoError(t, r.collectRBAC(ctx, tnt))
				before := tnt.DeepCopy()
				if revoked {
					switch kind {
					case "tenantowner-delete":
						require.NoError(t, r.Delete(ctx, dynamic))
					case "tenantowner-selector", "serviceaccount":
						require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(dynamic), dynamic))
						dynamic.SetLabels(nil)
						require.NoError(t, r.Update(ctx, dynamic))
					case "administrator":
						configuration.administrators = nil
						r.Configuration = configuration
					}
				}
				latest := &capsulev1beta2.Tenant{}
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), latest))
				require.Equal(t, before.Generation, latest.Generation)
				require.Equal(t, latest.Generation, latest.Status.ObservedGeneration)
				require.Equal(t, before.Status.Owners, latest.Status.Owners, "external revocation leaves evaluated status stale")
				require.Greater(t, len(latest.Status.Owners), len(latest.Spec.Owners))
				_, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)})
				require.NoError(t, err)
				assertBindingSubject(t, ctx, r.Client, ns.Name, "alice", true)
				assertBindingSubject(t, ctx, r.Client, ns.Name, subject, false)
				assertBindingSubject(t, ctx, r.Client, foreign.Name, subject, false)
				// Dynamic grants and revocations converge on the authoritative Tenant path.
				require.NoError(t, r.reconcile(ctx, logr.Discard(), latest))
				assertBindingSubject(t, ctx, r.Client, ns.Name, subject, !revoked)
				assertBindingSubject(t, ctx, r.Client, ns.Name, "alice", true)
				assertBindingSubject(t, ctx, r.Client, foreign.Name, subject, false)
			})
		}
	}
}

func TestNamespaceProvisioningPlansPreserveExplicitProfilesAndPrecedence(t *testing.T) {
	tnt := provisioningTenant()
	tnt.Spec.Owners[0].ClusterRoles = []string{"view"}
	tnt.Status.Owners[0].ClusterRoles = []string{"view", "admin"}
	binding := rbac.AdditionalRoleBindingsSpec{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: "alice"}}, Labels: map[string]string{"source": "explicit"}}
	tnt.Spec.AdditionalRoleBindings = []rbac.AdditionalRoleBindingsSpec{binding}
	selected := binding
	selected.Labels = map[string]string{"source": "rule"}
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{selected}}}}
	before := tnt.DeepCopy()
	for _, fast := range []bool{false, true} {
		plan := prepareNamespaceRoleBindings(tnt)
		if fast {
			plan = prepareNamespaceProvisioningRoleBindings(tnt)
		}
		for _, profile := range []string{"plain", "selected"} {
			bindings, err := plan.forNamespace(&corev1.Namespace{Name: profile, Labels: map[string]string{"profile": profile}})
			require.NoError(t, err)
			want := 2
			if fast {
				want = 1
			}
			require.Len(t, bindings, want)
			for _, binding := range bindings {
				if binding.ClusterRoleName == "view" {
					source := "explicit"
					if profile == "selected" {
						source = "rule"
					}
					require.Equal(t, source, binding.Labels["source"])
				} else {
					require.False(t, fast, "status-only roles for the same identity must not enter the fast plan")
				}
			}
		}
	}
	require.Equal(t, before, tnt, "plan construction must not mutate shared input")
}

func TestNamespaceQueueLeavesMetadataInventoryToTenant(t *testing.T) {
	for _, tenantFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("tenant-first=%v", tenantFirst), func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Labels: map[string]string{"managed": "old"}, Annotations: map[string]string{"managed": "old"}}}}
			selected, plain := cleanupOwnedNamespace(tnt, "selected", false), cleanupOwnedNamespace(tnt, "plain", false)
			selected.Labels["profile"], selected.Labels["user"] = "selected", "keep"
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, selected, plain, other, foreign)
			if tenantFirst {
				require.NoError(t, r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), tnt))
				require.NoError(t, r.Status().Update(ctx, tnt))
			}
			original := &corev1.Namespace{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(selected), original))
			var writes, namespaceReads, lists atomic.Int64
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Namespace); ok {
						namespaceReads.Add(1)
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
					lists.Add(1)
					return c.List(ctx, obj, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*corev1.Namespace); ok {
						writes.Add(1)
					}
					return c.Update(ctx, obj, opts...)
				},
			})
			r.provisioningReader = r.Client
			_, err := r.reconcileNamespaceProvisioning(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(selected)})
			require.NoError(t, err)
			require.Equal(t, int64(1), namespaceReads.Load(), "fast provisioning reuses its Namespace snapshot")
			require.Zero(t, lists.Load(), "fast provisioning must not discover dynamic owners or prune bindings")
			actual := &corev1.Namespace{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(selected), actual))
			require.Zero(t, writes.Load(), "fast provisioning must not publish untracked namespace metadata")
			require.True(t, reflect.DeepEqual(original, actual), "fast provisioning changed Namespace")
			assertBindingSubject(t, ctx, r.Client, selected.Name, "alice", true)
			latest := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), latest))
			latest.Spec.NamespaceOptions.AdditionalMetadataList = nil
			latest.Generation++
			require.NoError(t, r.Update(ctx, latest))
			require.NoError(t, r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), latest))
			for _, ns := range []*corev1.Namespace{selected, plain, foreign} {
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(ns), actual))
				require.NotContains(t, actual.Labels, "managed")
				require.NotContains(t, actual.Annotations, "managed")
				if ns.Name == selected.Name {
					require.Equal(t, "keep", actual.Labels["user"])
				}
			}
		})
	}
}
