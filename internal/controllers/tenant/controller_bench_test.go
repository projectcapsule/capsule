// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

// benchmarkRetainedFinalizers returns n distinct finalizer names, the CRD maximum being 64.
func benchmarkRetainedFinalizers(n int) []string {
	finalizers := make([]string, n)
	for i := range finalizers {
		finalizers[i] = fmt.Sprintf("example.com/retained-%d", i)
	}

	return finalizers
}

func BenchmarkControllerNamespaceProvisioning(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, count := range []int{1, 32} {
			b.Run(fmt.Sprintf("tenants=%d/namespaces=%d", tenants, count), func(b *testing.B) {
				tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid",
					Status: capsulev1beta2.TenantStatus{Owners: rbac.OwnerStatusListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}}},
					Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}},
						Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{
							ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: "User", Name: "reader"}},
						}}},
					}}},
				}
				objects := []client.Object{tnt}
				for i := range count {
					ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("team-%d", i), false)
					if i%2 == 0 {
						ns.Labels["profile"] = "reader"
					}
					objects = append(objects, ns)
				}
				for i := 1; i < tenants; i++ {
					other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i), UID: types.UID(fmt.Sprintf("unrelated-%d", i))}
					objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
				}
				r, _ := namespaceCleanupFixture(b, objects...)
				calls := &mockclient.CallCounter{}
				r.Client = interceptor.NewClient(provisioningDefaultingClient(r.Client.(client.WithWatch)), calls.Interceptors())
				r.reader = r.Client
				if err := r.reconcileActiveTenantNamespaces(b.Context(), logr.Discard(), tnt); err != nil {
					b.Fatal(err)
				}
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if err := r.reconcileActiveTenantNamespaces(b.Context(), logr.Discard(), tnt); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
			})
		}
	}
}

func BenchmarkControllerTenant(b *testing.B) {
	for _, kind := range []string{"tenant", "resource-quotas", "namespace-cleanup", "namespace-cleanup-missing-tenant", "namespace-cleanup-recreated-tenant", "namespace-cleanup-grace", "namespace-cleanup-retained"} {
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
				if kind == "tenant" {
					tnt.Spec.NetworkPolicies.Items = []networkingv1.NetworkPolicySpec{{PodSelector: metav1.LabelSelector{}}}
					tnt.Spec.LimitRanges.Items = []corev1.LimitRangeSpec{{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}}}}}
					tnt.Spec.Rules[0].NamespaceRuleBodyNamespace = &rules.NamespaceRuleBodyNamespace{Quota: []rules.ResourceQuotaRule{{Name: "compute", Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("100")}}}}
				}
				objects := []client.Object{other, cleanupOwnedNamespace(other, "unrelated", false)}
				switch kind {
				case "namespace-cleanup-missing-tenant":
				case "namespace-cleanup-recreated-tenant":
					replacement := tnt.DeepCopy()
					replacement.UID = "replacement-tenant-uid"
					objects = append(objects, replacement)
				default:
					objects = append(objects, tnt)
				}
				cleanup := strings.HasPrefix(kind, "namespace-cleanup")
				if cleanup {
					// Read through the real getter, as the controller does on every pass.
					cfg := &capsulev1beta2.CapsuleConfiguration{Name: "default"}
					switch kind {
					case "namespace-cleanup-grace":
						// The namespaces were deleted an hour ago: still within the grace period.
						cfg.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: 2 * time.Hour}
					case "namespace-cleanup-retained":
						cfg.Spec.NamespaceCleanup.RetainedFinalizers = benchmarkRetainedFinalizers(64)
					}
					objects = append(objects, cfg)
				}
				for i := range count {
					name := fmt.Sprintf("team-%d", i)
					ns := cleanupOwnedNamespace(tnt, name, cleanup)
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
				c := interceptor.NewClient(provisioningDefaultingClient(base.(client.WithWatch)), calls.Interceptors())
				r.Client = c
				r.reader = c
				// Configuration reads are informer-backed in the manager, not API
				// calls: count them apart from the API call counter.
				configReads := 0
				if cleanup {
					configClient := interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
						Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*capsulev1beta2.CapsuleConfiguration); ok {
								configReads++
							}
							return c.Get(ctx, key, obj, opts...)
						},
					})
					r.Configuration = configuration.NewCapsuleConfiguration(b.Context(), configClient, configClient, &rest.Config{}, "default")
				}
				var controller reconcile.Reconciler = r
				req := reconcile.Request{Name: tnt.Name}
				if kind == "resource-quotas" {
					controller = reconcile.Func(r.reconcileResourceQuotas)
				}
				if cleanup {
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
				case "namespace-cleanup", "namespace-cleanup-missing-tenant", "namespace-cleanup-recreated-tenant", "namespace-cleanup-retained":
					if len(dyn.Actions()) == 0 {
						b.Fatal("cleanup did not scan resources")
					}
				case "namespace-cleanup-grace":
					if len(dyn.Actions()) != 0 {
						b.Fatal("cleanup ran within the grace period")
					}
				}
				calls.Reset()
				configReads = 0
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
				if cleanup {
					b.ReportMetric(float64(configReads)/float64(b.N), "config-read/op")
				}
			})
		}
	}
}
