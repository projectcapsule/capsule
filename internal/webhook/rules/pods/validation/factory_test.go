// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
)

func podRulesForTest() *podRules {
	return &podRules{
		regexCache: cache.NewRegexCache(),
	}
}

func TestPodResourceRulesSkipSubresources(t *testing.T) {
	t.Parallel()

	h := podRulesForTest()
	h.rules = []podRuleValidator{{evaluate: h.validateResources}}

	ratio := resource.MustParse("1.5")
	enforce := []*apirules.NamespaceRuleEnforceBody{{
		Action: apirules.ActionTypeDeny,
		Workloads: apirules.NamespaceRuleEnforceWorkloadsBody{
			Resources: &apirules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]apirules.WorkloadResourceLimitPolicy{
					corev1.ResourceMemory: {
						Policy: apirules.WorkloadResourceLimitPolicyRatio,
						Value:  &ratio,
					},
				},
			},
		},
	}}
	pod := resourceValidationPod("1Gi", "2Gi")
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "ephemeralcontainers"}}

	if err := h.validatePodRules(
		context.Background(),
		req,
		pod,
		&capsulev1beta2.Tenant{},
		nil,
		enforce,
	); err != nil {
		t.Fatalf("validatePodRules() error = %v, resource rules must skip subresources", err)
	}
}
