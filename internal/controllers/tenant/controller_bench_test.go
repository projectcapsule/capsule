// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkControllerTenant(b *testing.B) {
	for _, kind := range []string{"tenant", "resource-quotas", "namespace-cleanup"} {
		for _, count := range []int{1, 32} {
			b.Run(fmt.Sprintf("%s/namespaces=%d", kind, count), func(b *testing.B) {
				tnt := &capsulev1beta2.Tenant{
					Name: "tenant-a",
					UID:  "tenant-a-uid",
					Spec: capsulev1beta2.TenantSpec{
						Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
						Rules: []*rules.NamespaceRuleBodyTenant{{
							NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}},
							Permissions: rules.NamespaceRulePermissionBody{
								Bindings: []rbac.AdditionalRoleBindingsSpec{{
									ClusterRoleName: "view",
									Subjects:        []rbacv1.Subject{{Kind: "User", Name: "profile-reader"}},
								}},
							},
						}},
						ResourceQuota: api.ResourceQuotaSpec{
							Scope: api.ResourceQuotaScopeNamespace,
							Items: []corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10")}}},
						},
					},
				}
				other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
				objects := []client.Object{tnt, other, cleanupOwnedNamespace(other, "unrelated", false)}
				for i := range count {
					name := fmt.Sprintf("team-%d", i)
					ns := cleanupOwnedNamespace(tnt, name, kind == "namespace-cleanup")
					if i%2 == 0 {
						ns.Labels["profile"] = "reader"
					}
					ns.UID = types.UID(name)
					objects = append(objects, ns)
					tnt.Status.Namespaces = append(tnt.Status.Namespaces, name)
					tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{Name: name})
				}
				r, dyn := namespaceCleanupFixture(b, objects...)
				for _, add := range []func(*runtime.Scheme) error{nodev1.AddToScheme, schedulingv1.AddToScheme, storagev1.AddToScheme} {
					if err := add(r.Scheme()); err != nil {
						b.Fatal(err)
					}
				}
				calls := &mockclient.CallCounter{}
				base := r.Client
				c := interceptor.NewClient(base.(client.WithWatch), calls.Interceptors())
				r.Client = c
				r.reader = c
				var controller reconcile.Reconciler = r
				req := reconcile.Request{Name: tnt.Name}
				if kind == "resource-quotas" {
					controller = reconcile.Func(r.reconcileResourceQuotas)
				}
				if kind == "namespace-cleanup" {
					controller = reconcile.Func(r.reconcileNamespaceCleanup)
					req.Name = "team-0"
				}
				if _, err := controller.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				switch kind {
				case "tenant":
					bindings := &rbacv1.RoleBindingList{}
					if err := base.List(b.Context(), bindings, client.InNamespace("team-0")); err != nil {
						b.Fatal(err)
					}
					if len(bindings.Items) == 0 {
						b.Fatal("missing rolebindings")
					}
					assertBindingSubject(b, b.Context(), base, "team-0", "profile-reader", true)
					if count > 1 {
						assertBindingSubject(b, b.Context(), base, "team-1", "profile-reader", false)
					}
					assertBindingSubject(b, b.Context(), base, "unrelated", "alice", false)
				case "resource-quotas":
					quotas := &corev1.ResourceQuotaList{}
					if err := base.List(b.Context(), quotas); err != nil {
						b.Fatal(err)
					}
					if len(quotas.Items) != count {
						b.Fatalf("got %d quotas, want %d", len(quotas.Items), count)
					}
				case "namespace-cleanup":
					if len(dyn.Actions()) == 0 {
						b.Fatal("cleanup did not scan resources")
					}
				}
				calls.Reset()
				b.ReportAllocs()
				dynamicLists := 0
				for b.Loop() {
					dyn.ClearActions()
					if _, err := controller.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
					for _, action := range dyn.Actions() {
						if action.GetVerb() == "list" {
							dynamicLists++
						}
					}
				}
				calls.Report(b)
				b.ReportMetric(float64(dynamicLists)/float64(b.N), "dynamic-LIST/op")
			})
		}
	}
}
