// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
)

func BenchmarkControllerLimitRanges(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, namespaces := range []int{1, 32} {
			for _, items := range []int{1, 8} {
				b.Run(fmt.Sprintf("tenants=%d/namespaces=%d/items=%d", tenants, namespaces, items), func(b *testing.B) {
					tnt := provisioningTenant()
					for range items {
						tnt.Spec.LimitRanges.Items = append(tnt.Spec.LimitRanges.Items, corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
							Type: corev1.LimitTypeContainer, Default: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
							DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")},
						}}})
					}
					objects := []client.Object{tnt}
					for i := range namespaces {
						ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("team-%d", i), false)
						objects = append(objects, ns)
						tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{meta.NewReadyCondition(ns)}})
					}
					for i := 1; i < tenants; i++ {
						other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i), UID: types.UID(fmt.Sprintf("unrelated-%d", i))}
						objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
					}
					r, _ := namespaceCleanupFixture(b, objects...)
					calls := &mockclient.CallCounter{}
					r.Client = interceptor.NewClient(r.Client.(client.WithWatch), calls.Interceptors())
					if err := r.syncLimitRanges(b.Context(), logr.Discard(), tnt); err != nil {
						b.Fatal(err)
					}
					calls.Reset()
					b.ReportAllocs()
					for b.Loop() {
						if err := r.syncLimitRanges(b.Context(), logr.Discard(), tnt); err != nil {
							b.Fatal(err)
						}
					}
					calls.Report(b)
				})
			}
		}
	}
}

func BenchmarkControllerNamespaceQueueOwners(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, owners := range []int{0, 32} {
			for _, ruleCount := range []int{1, 32} {
				for _, parallel := range []bool{false, true} {
					b.Run(fmt.Sprintf("tenants=%d/owners=%d/rules=%d/parallel=%v", tenants, owners, ruleCount, parallel), func(b *testing.B) {
						tnt := provisioningTenant()
						tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Labels: map[string]string{"managed": "value"}, Annotations: map[string]string{"managed": "value"}}}}
						for i := range owners {
							tnt.Status.Owners = append(tnt.Status.Owners, rbac.CoreOwnerSpec{Kind: rbac.UserOwner, Name: fmt.Sprintf("dynamic-%d", i), ClusterRoles: []string{"view"}})
						}
						for range ruleCount {
							tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "explicit-reader"}}}}}})
						}
						objects := []client.Object{tnt}
						requests := make([]reconcile.Request, max(16, 2*runtime.GOMAXPROCS(0)))
						for i := range requests {
							ns := cleanupOwnedNamespace(tnt, fmt.Sprintf("namespace-%d", i), false)
							if i%2 == 0 {
								ns.Labels["profile"] = "selected"
							}
							objects = append(objects, ns)
							requests[i] = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}
						}
						for i := 1; i < tenants; i++ {
							other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i), UID: types.UID(fmt.Sprintf("unrelated-%d", i))}
							objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
						}
						r, _ := namespaceCleanupFixture(b, objects...)
						calls := &mockclient.CallCounter{}
						r.Client = interceptor.NewClient(provisioningDefaultingClient(r.Client.(client.WithWatch)), calls.Interceptors())
						r.provisioningReader = r.Client
						for _, request := range requests {
							if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
								b.Fatal(err)
							}
						}
						calls.Reset()
						b.ReportAllocs()
						b.ResetTimer()
						run := func(request reconcile.Request) {
							result, err := r.reconcileNamespaceProvisioning(b.Context(), request)
							if err != nil || result.RequeueAfter != 0 {
								b.Fatalf("provisioning did not complete: %+v, %v", result, err)
							}
						}
						if parallel {
							var workers atomic.Uint64
							b.RunParallel(func(pb *testing.PB) {
								worker := int(workers.Add(1) - 1)
								for i := 0; pb.Next(); i++ {
									run(requests[2*worker+i%2])
								}
							})
						} else {
							for i := 0; b.Loop(); i++ {
								run(requests[i%len(requests)])
							}
						}
						b.StopTimer()
						calls.Report(b)
					})
				}
			}
		}
	}
}

func BenchmarkControllerNamespaceQueuePromotions(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, promotions := range []int{0, 32} {
			b.Run(fmt.Sprintf("tenants=%d/promotions=%d", tenants, promotions), func(b *testing.B) {
				tnt := provisioningTenant()
				ns := cleanupOwnedNamespace(tnt, "new-namespace", false)
				tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "privileged"}}, Permissions: rules.NamespaceRulePermissionBody{Promotions: []*rules.NamespaceRulePromotionRule{{ClusterRoles: []string{"view"}}}}}}
				for i := range promotions {
					promotion := rbac.PromotionSpec{ClusterRoles: []string{"view"}, Targets: []string{ns.Name}}
					promotion.Kind, promotion.Name = rbac.ServiceAccountOwner, fmt.Sprintf("system:serviceaccount:source:promoted-%d", i)
					tnt.Status.Promotions = append(tnt.Status.Promotions, promotion)
				}
				objects := []client.Object{tnt, ns}
				for i := 1; i < tenants; i++ {
					other := &capsulev1beta2.Tenant{Name: fmt.Sprintf("unrelated-%d", i), UID: types.UID(fmt.Sprintf("unrelated-%d", i))}
					objects = append(objects, other, cleanupOwnedNamespace(other, other.Name+"-ns", false))
				}
				r, _ := namespaceCleanupFixture(b, objects...)
				calls := &mockclient.CallCounter{}
				r.Client = interceptor.NewClient(provisioningDefaultingClient(r.Client.(client.WithWatch)), calls.Interceptors())
				r.provisioningReader = r.Client
				request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}
				if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
					b.Fatal(err)
				}
				calls.Reset()
				b.ReportAllocs()
				for b.Loop() {
					if _, err := r.reconcileNamespaceProvisioning(b.Context(), request); err != nil {
						b.Fatal(err)
					}
				}
				calls.Report(b)
			})
		}
	}
}
