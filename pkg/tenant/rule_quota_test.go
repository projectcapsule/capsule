// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestRuleGlobalResourceQuota(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("tenant-uid")},
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{
					Name: "shared-compute",
					ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
					}},
				}},
			},
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "paid"}},
		}}},
	}

	quota := RuleGlobalResourceQuota(tnt, 0, 0)
	if quota.Name != RuleGlobalResourceQuotaName(tnt, "shared-compute") {
		t.Fatalf("generated name = %q, want deterministic helper name", quota.Name)
	}
	if len(quota.Spec.NamespaceSelectors) != 1 {
		t.Fatalf("namespace selectors = %d, want 1", len(quota.Spec.NamespaceSelectors))
	}
	selector := quota.Spec.NamespaceSelectors[0].LabelSelector
	if selector.MatchLabels["tier"] != "paid" || selector.MatchLabels[meta.TenantLabel] != tnt.Name {
		t.Fatalf("generated selector = %#v, want tenant and rule labels", selector)
	}
	if got := quota.Spec.Quota.Hard[corev1.ResourceRequestsMemory]; got.Cmp(resource.MustParse("16Gi")) != 0 {
		t.Fatalf("generated quota hard = %s, want 16Gi", got.String())
	}
}

func TestRuleGlobalResourceQuotaNameIsStableAcrossRuleChanges(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("tenant-uid")},
	}

	want := RuleGlobalResourceQuotaName(tnt, "shared-compute")
	if want != "tenant-a-shared-compute" {
		t.Fatalf("generated name = %q, want Tenant and quota name", want)
	}
	if got := RuleGlobalResourceQuotaName(tnt, "shared-compute"); got != want {
		t.Fatalf("same quota name generated %q, want %q", got, want)
	}
	if got := RuleGlobalResourceQuotaName(tnt, "service-count"); got == want {
		t.Fatalf("different quota names generated the same object name %q", got)
	}

	recreated := tnt.DeepCopy()
	recreated.UID = types.UID("replacement-tenant-uid")
	if got := RuleGlobalResourceQuotaName(recreated, "shared-compute"); got != want {
		t.Fatalf("recreated Tenant generated %q, want stable name %q", got, want)
	}

	longTenant := tnt.DeepCopy()
	longTenant.Name = strings.Repeat("a", 250)
	if err := ValidateRuleGlobalResourceQuotaName(longTenant, "shared-compute"); err == nil {
		t.Fatal("overlong generated name was accepted")
	}
}
