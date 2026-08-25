// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
)

func TestValidateRuleQuotaUpdates(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	oldTenant := ruleQuotaTenant("5")
	desiredGlobalQuota := tenantutils.RuleGlobalResourceQuota(oldTenant, 0, 0)
	globalQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantutils.RuleGlobalResourceQuotaName(oldTenant, "services"),
			UID:  types.UID("global-quota-uid"),
		},
		Spec: desiredGlobalQuota.Spec,
		Status: capsulev1beta2.GlobalResourceQuotaStatus{
			Total: capsulev1beta2.GlobalResourceQuotaUsage{Used: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("4"),
			}},
		},
	}

	t.Run("observed usage", func(t *testing.T) {
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(globalQuota).Build()
		response := validateRuleQuotaUpdates(context.Background(), reader, ruleQuotaTenant("3"), oldTenant)
		assertRuleQuotaDenied(t, response, "cannot be reduced to 3 while 4 is allocated")
	})

	t.Run("inflight allocation", func(t *testing.T) {
		quota := globalQuota.DeepCopy()
		quota.Status.Total.Used[corev1.ResourceServices] = resource.MustParse("2")
		ledger := ruleQuotaLedger(quota, "4")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(quota, ledger).Build()

		response := validateRuleQuotaUpdates(context.Background(), reader, ruleQuotaTenant("3"), oldTenant)
		assertRuleQuotaDenied(t, response, "cannot be reduced to 3 while 4 is allocated")
	})

	t.Run("equal to allocated", func(t *testing.T) {
		quota := globalQuota.DeepCopy()
		quota.Status.Total.Used[corev1.ResourceServices] = resource.MustParse("2")
		ledger := ruleQuotaLedger(quota, "4")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(quota, ledger).Build()

		if response := validateRuleQuotaUpdates(context.Background(), reader, ruleQuotaTenant("4"), oldTenant); response != nil {
			t.Fatalf("hard limit equal to allocated usage was rejected: %#v", response)
		}
	})

	t.Run("removal with allocation", func(t *testing.T) {
		old := oldTenant.DeepCopy()
		old.Spec.Rules[0].Quota[0].Hard[corev1.ResourceRequestsCPU] = resource.MustParse("1")
		quota := tenantutils.RuleGlobalResourceQuota(old, 0, 0)
		quota.UID = types.UID("removal-global-quota-uid")
		quota.Status.Total.Used = corev1.ResourceList{
			corev1.ResourceServices: resource.MustParse("2"),
		}
		ledger := ruleQuotaLedger(quota, "4")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(quota, ledger).Build()

		updated := old.DeepCopy()
		delete(updated.Spec.Rules[0].Quota[0].Hard, corev1.ResourceServices)
		if response := validateRuleQuotaUpdates(context.Background(), reader, updated, old); response != nil {
			t.Fatalf("allocated rule quota resource removal was rejected: %#v", response)
		}
	})

	t.Run("scope change with decrease", func(t *testing.T) {
		oldScoped := ruleQuotaTenant("8")
		oldScoped.Spec.Rules[0].NamespaceSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"company.example/tier": "application"},
		}
		quota := tenantutils.RuleGlobalResourceQuota(oldScoped, 0, 0)
		quota.UID = types.UID("scoped-global-quota-uid")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(quota).Build()

		response := validateRuleQuotaUpdates(context.Background(), reader, ruleQuotaTenant("0"), oldScoped)
		assertRuleQuotaDenied(
			t,
			response,
			`cannot be reduced from 8 to 0 while namespace selectors are changing`,
		)
	})

	t.Run("unchanged", func(t *testing.T) {
		reader := &failingReader{}
		if response := validateRuleQuotaUpdates(context.Background(), reader, oldTenant.DeepCopy(), oldTenant); response != nil {
			t.Fatalf("unchanged hard limit performed quota validation: %#v", response)
		}
	})
}

type failingReader struct{ client.Reader }

func (f *failingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return context.Canceled
}

func ruleQuotaTenant(hard string) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("tenant-a-uid")},
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{
					Name: "services",
					ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourceServices: resource.MustParse(hard),
					}},
				}},
			},
		}}},
	}
}

func ruleQuotaLedger(
	quota *capsulev1beta2.GlobalResourceQuota,
	allocated string,
) *capsulev1beta2.QuantityLedger {
	return &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: configuration.ControllerNamespace(),
			Name:      quota.GetLedgerName(),
		},
		Spec: capsulev1beta2.QuantityLedgerSpec{
			TargetRef: capsulev1beta2.QuantityLedgerTargetRef{UID: quota.UID},
		},
		Status: capsulev1beta2.QuantityLedgerStatus{
			ResourceQuota: &capsulev1beta2.QuantityLedgerResourceQuotaStatus{
				Allocated: corev1.ResourceList{
					corev1.ResourceServices: resource.MustParse(allocated),
				},
			},
		},
	}
}

func assertRuleQuotaDenied(t *testing.T, response *admission.Response, message string) {
	t.Helper()

	if response == nil || response.Allowed || response.Result == nil {
		t.Fatal("rule quota update was accepted")
	}
	if !strings.Contains(response.Result.Message, message) {
		t.Fatalf("denial message = %q, want substring %q", response.Result.Message, message)
	}
}
