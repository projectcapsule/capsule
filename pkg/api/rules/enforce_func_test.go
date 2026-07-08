// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	corev1 "k8s.io/api/core/v1"
)

func TestActionTypeOrDefault(t *testing.T) {
	t.Parallel()

	if got := rules.ActionType("").OrDefault(); got != rules.ActionTypeDeny {
		t.Fatalf("empty action OrDefault() = %q, want deny", got)
	}
	if got := rules.ActionTypeAllow.OrDefault(); got != rules.ActionTypeAllow {
		t.Fatalf("allow OrDefault() = %q, want allow", got)
	}
}

func TestNamespaceRuleEnforceBodyWorkloadTargets(t *testing.T) {
	t.Parallel()

	empty := rules.NamespaceRuleEnforceBody{}
	if !empty.GetWorkloadTargets(rules.ValidateContainers) {
		t.Fatalf("empty workload targets should match any target")
	}
	if !empty.WorkloadTargetsAny(rules.ValidateContainers, rules.ValidateVolumes) {
		t.Fatalf("empty workload targets should match any candidate")
	}

	enforce := rules.NamespaceRuleEnforceBody{
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
		},
	}
	if !enforce.GetWorkloadTargets(rules.ValidateContainers) {
		t.Fatalf("GetWorkloadTargets(containers) = false, want true")
	}
	if enforce.GetWorkloadTargets(rules.ValidateVolumes) {
		t.Fatalf("GetWorkloadTargets(volumes) = true, want false")
	}
	if !enforce.WorkloadTargetsAny(rules.ValidateVolumes, rules.ValidateContainers) {
		t.Fatalf("WorkloadTargetsAny() = false, want true when one target matches")
	}
	if enforce.WorkloadTargetsAny(rules.ValidateVolumes, rules.ValidateInitContainers) {
		t.Fatalf("WorkloadTargetsAny() = true, want false when no target matches")
	}
}

func TestImagePullPolicySpecString(t *testing.T) {
	t.Parallel()

	if got := rules.ImagePullPolicySpec(corev1.PullAlways).String(); got != "Always" {
		t.Fatalf("String() = %q, want Always", got)
	}
}
