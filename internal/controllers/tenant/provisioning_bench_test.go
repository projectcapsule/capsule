// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
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
