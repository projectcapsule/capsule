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

func TestValidateRuleQuotaUpdatesRejectsHardBelowUsageOrAllocation(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	oldTenant := ruleQuotaTenant("5")
	globalQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantutils.RuleGlobalResourceQuotaName(oldTenant, "services"),
			UID:  types.UID("global-quota-uid"),
		},
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
