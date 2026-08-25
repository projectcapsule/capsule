// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
)

func TestSyncGlobalResourceQuotasGeneratesAndPrunesRuleQuotas(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("tenant-uid")},
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{
					Name: "shared-compute",
					ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("8"),
					}},
				}},
			},
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "paid"}},
		}}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	manager := &Manager{Client: cl}

	if err := manager.syncGlobalResourceQuotas(context.Background(), tnt); err != nil {
		t.Fatalf("syncGlobalResourceQuotas() error = %v", err)
	}

	key := client.ObjectKey{Name: tenantutils.RuleGlobalResourceQuotaName(tnt, "shared-compute")}
	generated := &capsulev1beta2.GlobalResourceQuota{}
	if err := cl.Get(context.Background(), key, generated); err != nil {
		t.Fatalf("get generated GlobalResourceQuota: %v", err)
	}
	if generated.Labels[meta.RuleQuotaLabel] != "shared-compute" {
		t.Fatalf("rule quota label = %q, want shared-compute", generated.Labels[meta.RuleQuotaLabel])
	}
	if generated.Labels[meta.NewManagedByCapsuleLabel] != meta.ValueController {
		t.Fatalf(
			"managed-by label = %q, want %q",
			generated.Labels[meta.NewManagedByCapsuleLabel],
			meta.ValueController,
		)
	}
	if !metav1.IsControlledBy(generated, tnt) {
		t.Fatalf("generated GlobalResourceQuota is not controlled by Tenant %q", tnt.Name)
	}
	selector := generated.Spec.NamespaceSelectors[0].LabelSelector
	if selector.MatchLabels[meta.TenantLabel] != tnt.Name || selector.MatchLabels["tier"] != "paid" {
		t.Fatalf("generated selector = %#v", selector)
	}

	generated.Annotations = map[string]string{"test.projectcapsule.dev/identity": "preserved"}
	if err := cl.Update(context.Background(), generated); err != nil {
		t.Fatalf("annotate generated GlobalResourceQuota: %v", err)
	}

	sharedRule := tnt.Spec.Rules[0]
	sharedRule.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "enterprise"}}
	sharedRule.Quota[0].Hard[corev1.ResourceRequestsCPU] = resource.MustParse("12")
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
		{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{
					Name: "service-count",
					ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourceServices: resource.MustParse("5"),
					}},
				}},
			},
		},
		sharedRule,
	}
	if err := manager.syncGlobalResourceQuotas(context.Background(), tnt); err != nil {
		t.Fatalf("sync reordered GlobalResourceQuotas: %v", err)
	}

	updated := &capsulev1beta2.GlobalResourceQuota{}
	if err := cl.Get(context.Background(), key, updated); err != nil {
		t.Fatalf("get stable GlobalResourceQuota after reorder: %v", err)
	}
	if updated.Annotations["test.projectcapsule.dev/identity"] != "preserved" {
		t.Fatal("generated GlobalResourceQuota was replaced after rule reorder")
	}
	updatedSelector := updated.Spec.NamespaceSelectors[0].LabelSelector
	if updatedSelector.MatchLabels["tier"] != "enterprise" {
		t.Fatalf("updated selector = %#v, want tier=enterprise", updatedSelector)
	}
	if got := updated.Spec.Quota.Hard[corev1.ResourceRequestsCPU]; got.Cmp(resource.MustParse("12")) != 0 {
		t.Fatalf("updated hard requests.cpu = %s, want 12", got.String())
	}

	list := &capsulev1beta2.GlobalResourceQuotaList{}
	if err := cl.List(context.Background(), list); err != nil {
		t.Fatalf("list generated GlobalResourceQuotas: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("generated GlobalResourceQuotas = %d, want 2", len(list.Items))
	}

	tnt.Spec.Rules = nil
	if err := manager.syncGlobalResourceQuotas(context.Background(), tnt); err != nil {
		t.Fatalf("prune GlobalResourceQuota: %v", err)
	}
	err := cl.Get(context.Background(), key, &capsulev1beta2.GlobalResourceQuota{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("get pruned GlobalResourceQuota error = %v, want NotFound", err)
	}
	list = &capsulev1beta2.GlobalResourceQuotaList{}
	if err := cl.List(context.Background(), list); err != nil {
		t.Fatalf("list pruned GlobalResourceQuotas: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("GlobalResourceQuotas after prune = %d, want 0", len(list.Items))
	}
}
