// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestReconcileExcludesQuotaFromRuleStatus(t *testing.T) {
	t.Parallel()

	unnamedQuota := rules.ResourceQuotaRule{
		ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceRequestsCPU: resource.MustParse("1"),
		}},
	}
	instance := &capsulev1beta2.RuleStatus{
		Spec: []*rules.NamespaceRuleBodyNamespace{
			{Quota: []rules.ResourceQuotaRule{unnamedQuota}},
			{
				Quota:   []rules.ResourceQuotaRule{unnamedQuota},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny},
			},
		},
	}

	if err := (Manager{}).reconcile(context.Background(), instance); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}
	if len(instance.Status.Rules) != 1 {
		t.Fatalf("status rules = %d, want one enforcement rule", len(instance.Status.Rules))
	}
	if len(instance.Status.Rules[0].Quota) != 0 {
		t.Fatalf("status quota = %#v, want none", instance.Status.Rules[0].Quota)
	}
	if instance.Status.Rules[0].Enforce == nil {
		t.Fatal("status enforcement rule was removed")
	}
}

func TestRemoveQuotaDefinitionsCleansLegacyStatus(t *testing.T) {
	t.Parallel()

	status := &capsulev1beta2.RuleStatusStatus{
		Rule: rules.NamespaceRuleBodyNamespace{Quota: []rules.ResourceQuotaRule{{}}},
		Rules: []*rules.NamespaceRuleBodyNamespace{
			nil,
			{Quota: []rules.ResourceQuotaRule{{}}},
		},
	}

	if changed := removeQuotaDefinitions(status); !changed {
		t.Fatal("legacy quota definitions were not reported as changed")
	}

	if len(status.Rule.Quota) != 0 || len(status.Rules[1].Quota) != 0 {
		t.Fatalf("legacy quota definitions were not removed: %#v", status)
	}
	if changed := removeQuotaDefinitions(status); changed {
		t.Fatal("clean status was reported as changed")
	}
}
