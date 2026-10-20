// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestCollectPromotionsSkipsDiscoveryWithoutPromotions(t *testing.T) {
	tnt := tenantObject("tenant-a", withStatusNamespaces("ns-a"))
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{nil, {}, {
		NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "invalid", Operator: "invalid"}}},
	}}
	base := tenantFakeClient(t, tnt).(client.WithWatch)
	var lists atomic.Int64
	c := interceptor.NewClient(base, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
		lists.Add(1)
		return errors.New("discovery must not run")
	}})
	got, err := tenant.CollectPromotions(t.Context(), c, tnt, nil)
	if err != nil || len(got) != 0 || lists.Load() != 0 {
		t.Fatalf("promotions=%v, error=%v, lists=%d", got, err, lists.Load())
	}
}

func promotionFixture(tb testing.TB, unrelated, namespaces int) (*capsulev1beta2.Tenant, client.WithWatch) {
	tb.Helper()
	tnt := tenantObject("tenant-a", withUID("tenant-a-uid"))
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{nil, {}, {
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}},
		Permissions:       rules.NamespaceRulePermissionBody{Promotions: []*rules.NamespaceRulePromotionRule{{ClusterRoles: []string{"view"}}}},
	}}
	objects := []client.Object{tnt}
	for team := 0; team <= unrelated; team++ {
		name, uid := tnt.Name, tnt.UID
		if team > 0 {
			name, uid = fmt.Sprintf("other-%d", team), types.UID(fmt.Sprintf("other-%d-uid", team))
		}
		for i := range namespaces {
			ns := fmt.Sprintf("%s-ns-%d", name, i)
			if team == 0 {
				tnt.Status.Namespaces = append(tnt.Status.Namespaces, ns)
			}
			objects = append(objects, &corev1.Namespace{Name: ns, Labels: map[string]string{"profile": "selected", corev1.LabelMetadataName: ns}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: uid}}},
				&corev1.ServiceAccount{Name: "builder", Namespace: ns, Labels: map[string]string{meta.ServiceAccountPromotionLabel: meta.ValueTrue}})
		}
	}
	return tnt, tenantFakeClient(tb, objects...).(client.WithWatch)
}

func TestCollectPromotionsUsesOwnedIndexedNamespaces(t *testing.T) {
	tnt, base := promotionFixture(t, 1, 2)
	// Stale status and a reused Tenant name are both insufficient for a grant.
	tnt.Status.Namespaces = append(tnt.Status.Namespaces, "other-1-ns-0", "reused")
	stale := &corev1.Namespace{Name: "reused", Labels: map[string]string{"profile": "selected"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: "deleted-tenant"}}}
	if err := base.Create(t.Context(), stale); err != nil {
		t.Fatal(err)
	}
	if err := base.Create(t.Context(), &corev1.ServiceAccount{Name: "builder", Namespace: stale.Name, Labels: map[string]string{meta.ServiceAccountPromotionLabel: meta.ValueTrue}}); err != nil {
		t.Fatal(err)
	}
	var namespaceLists, serviceAccountLists int
	c := interceptor.NewClient(base, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		options := (&client.ListOptions{}).ApplyOptions(opts)
		switch list.(type) {
		case *corev1.NamespaceList:
			namespaceLists++
			if options.FieldSelector == nil || options.FieldSelector.String() != tenant.NamespaceOwnerReferenceIndexerFieldName+"="+tnt.Name {
				t.Errorf("namespace lookup was not indexed: %+v", options)
			}
		case *corev1.ServiceAccountList:
			serviceAccountLists++
			if options.Namespace != "tenant-a-ns-0" && options.Namespace != "tenant-a-ns-1" {
				t.Errorf("foreign namespace read: %s", options.Namespace)
			}
		}
		return c.List(ctx, list, opts...)
	}})
	got, err := tenant.CollectPromotions(t.Context(), c, tnt, nil)
	if err != nil || len(got) != 2 || namespaceLists != 1 || serviceAccountLists != 2 {
		t.Fatalf("promotions=%v, error=%v, namespace LIST=%d, SA LIST=%d", got, err, namespaceLists, serviceAccountLists)
	}
	for _, p := range got {
		for _, target := range p.Targets {
			if target != "tenant-a-ns-0" && target != "tenant-a-ns-1" {
				t.Fatalf("cross-tenant target: %s", target)
			}
		}
	}
}

func BenchmarkCollectPromotions(b *testing.B) {
	for _, unrelated := range []int{0, 16} {
		for _, namespaces := range []int{1, 32} {
			for _, enabled := range []bool{false, true} {
				b.Run(fmt.Sprintf("unrelatedTenants=%d/namespaces=%d/enabled=%t", unrelated, namespaces, enabled), func(b *testing.B) {
					tnt, base := promotionFixture(b, unrelated, namespaces)
					if !enabled {
						tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{}}
					} else {
						tnt.Spec.Rules = tnt.Spec.Rules[2:]
					}
					var lists atomic.Int64
					c := interceptor.NewClient(base, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists.Add(1)
						return c.List(ctx, list, opts...)
					}})
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						if _, err := tenant.CollectPromotions(b.Context(), c, tnt, nil); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(lists.Load())/float64(b.N), "LIST/op")
				})
			}
		}
	}
}
