// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateRuleStatusBodyWorkloadResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    *rules.NamespaceRuleBodyNamespace
		wantErr string
	}{
		{
			name: "valid",
			body: workloadResourceRuleForValidation(
				[]rules.WorkloadValidationTarget{rules.ValidateContainers},
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyDefault, Value: validationQuantity("1Gi")},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio, Value: validationQuantity("1.5")},
			),
		},
		{
			name: "ratio below one",
			body: workloadResourceRuleForValidation(
				nil,
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyPreserve},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio, Value: validationQuantity("0.5")},
			),
			wantErr: "Ratio must be greater than or equal to 1",
		},
		{
			name: "ratio missing value",
			body: workloadResourceRuleForValidation(
				nil,
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyPreserve},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio},
			),
			wantErr: "Ratio requires a value",
		},
		{
			name: "unsupported target",
			body: workloadResourceRuleForValidation(
				[]rules.WorkloadValidationTarget{rules.ValidateEphemeralContainers},
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyPreserve},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio, Value: validationQuantity("1.5")},
			),
			wantErr: "does not support resource policies",
		},
		{
			name: "pod-level ephemeral storage",
			body: workloadResourceRuleForValidation(
				[]rules.WorkloadValidationTarget{rules.ValidatePod},
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyPreserve},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio, Value: validationQuantity("1.5")},
			),
			wantErr: "is not supported by pod-level resources",
		},
		{
			name: "ratio request removed",
			body: workloadResourceRuleForValidation(
				nil,
				rules.WorkloadResourceRequestPolicy{Policy: rules.WorkloadResourceRequestPolicyRemove},
				rules.WorkloadResourceLimitPolicy{Policy: rules.WorkloadResourceLimitPolicyRatio, Value: validationQuantity("1.5")},
			),
			wantErr: "requires a request which is removed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{tt.body})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRuleStatusBody() error = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateRuleStatusBody() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func workloadResourceRuleForValidation(
	targets []rules.WorkloadValidationTarget,
	requestPolicy rules.WorkloadResourceRequestPolicy,
	limitPolicy rules.WorkloadResourceLimitPolicy,
) *rules.NamespaceRuleBodyNamespace {
	name := corev1.ResourceMemory
	if len(targets) == 1 && targets[0] == rules.ValidatePod {
		name = corev1.ResourceEphemeralStorage
	}

	return &rules.NamespaceRuleBodyNamespace{
		Enforce: &rules.NamespaceRuleEnforceBody{
			Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
				Targets: targets,
				Resources: &rules.WorkloadResourceRules{
					Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{name: requestPolicy},
					Limits:   map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{name: limitPolicy},
				},
			},
		},
	}
}

func validationQuantity(value string) *resource.Quantity {
	quantity := resource.MustParse(value)

	return &quantity
}
