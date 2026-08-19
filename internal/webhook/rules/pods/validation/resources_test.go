// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateResourcesDenyRatioViolation(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("1Gi", "2Gi")

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeDeny, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking == nil {
		t.Fatal("validateResources() did not block a ratio violation")
	}
	if !strings.Contains(evaluation.Blocking.Message, "must not exceed 1536Mi") {
		t.Fatalf("blocking message = %q", evaluation.Blocking.Message)
	}
}

func TestValidateResourcesAllowsCompliantRatio(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("1Gi", "1280Mi")

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeDeny, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking != nil {
		t.Fatalf("validateResources() evaluation = %#v, want non-blocking", evaluation)
	}
}

func TestValidateResourcesAllowUsesPolicyAsAllowList(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()

	compliant, err := h.validateResources(resourceValidationPod("1Gi", "1280Mi"), []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeAllow, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources(compliant) error = %v", err)
	}
	if compliant == nil || compliant.Blocking != nil {
		t.Fatalf("validateResources(compliant) = %#v, want allowed", compliant)
	}

	violating, err := h.validateResources(resourceValidationPod("1Gi", "2Gi"), []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeAllow, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources(violating) error = %v", err)
	}
	if violating == nil || violating.Blocking == nil ||
		!strings.Contains(violating.Blocking.Message, "does not satisfy any allowed resource policy") {
		t.Fatalf("validateResources(violating) = %#v, want allow-list miss", violating)
	}
}

func TestValidateResourcesLaterAllowOverridesDeny(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("1Gi", "1792Mi")

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeDeny, "1.5"),
		resourceEnforcement(apirules.ActionTypeAllow, "2"),
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking != nil || evaluation.Final == nil ||
		evaluation.Final.Action != apirules.ActionTypeAllow {
		t.Fatalf("validateResources() = %#v, want later allow decision", evaluation)
	}
}

func TestValidateResourcesLaterPreserveClearsConstraint(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("1Gi", "2Gi")
	preserve := &apirules.NamespaceRuleEnforceBody{
		Action: apirules.ActionTypeAllow,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []apirules.WorkloadValidationTarget{apirules.ValidateContainers},
			Resources: &apirules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy{
					corev1.ResourceMemory: {Policy: apirules.WorkloadResourceLimitPolicyPreserve},
				},
			},
		},
	}

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeDeny, "1.5"),
		preserve,
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking != nil {
		t.Fatalf("validateResources() = %#v, want later Preserve to clear the constraint", evaluation)
	}
}

func TestValidateResourcesAuditsViolation(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("1Gi", "2Gi")

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeAudit, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking != nil || len(evaluation.Audits) != 1 {
		t.Fatalf("validateResources() = %#v, want one non-blocking audit", evaluation)
	}
}

func TestValidateResourcesRequiresRequestForRatio(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := resourceValidationPod("", "1Gi")

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{
		resourceEnforcement(apirules.ActionTypeDeny, "1.5"),
	})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking == nil ||
		!strings.Contains(evaluation.Blocking.Message, "requires a request greater than zero") {
		t.Fatalf("validateResources() = %#v, want missing-request violation", evaluation)
	}
}

func TestValidateResourcesHonorsTarget(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			},
		}},
		InitContainers: []corev1.Container{{
			Name: "init",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
			},
		}},
	}}

	enforce := resourceEnforcement(apirules.ActionTypeDeny, "1.5")
	enforce.Workloads.Targets = []apirules.WorkloadValidationTarget{apirules.ValidateContainers}

	evaluation, err := h.validateResources(pod, []*apirules.NamespaceRuleEnforceBody{enforce})
	if err != nil {
		t.Fatalf("validateResources() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking != nil {
		t.Fatalf("validateResources() = %#v, init container should be out of scope", evaluation)
	}
}

func resourceValidationPod(request, limit string) *corev1.Pod {
	resources := corev1.ResourceRequirements{}
	if request != "" {
		resources.Requests = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(request)}
	}
	if limit != "" {
		resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(limit)}
	}

	return &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name:      "app",
		Resources: resources,
	}}}}
}

func resourceEnforcement(action apirules.ActionType, ratio string) *apirules.NamespaceRuleEnforceBody {
	value := resource.MustParse(ratio)

	return &apirules.NamespaceRuleEnforceBody{
		Action: action,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []apirules.WorkloadValidationTarget{apirules.ValidateContainers},
			Resources: &apirules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy{
					corev1.ResourceMemory: {
						Policy: apirules.WorkloadResourceLimitPolicyRatio,
						Value:  &value,
					},
				},
			},
		},
	}
}
