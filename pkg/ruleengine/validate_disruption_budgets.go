// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"slices"

	policyv1 "k8s.io/api/policy/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateDisruptionBudgetRules(index int, workloads rules.NamespaceRuleEnforceWorkloadsBody) error {
	policy := workloads.DisruptionBudgets
	if policy == nil {
		return nil
	}

	path := fmt.Sprintf("rules[%d].enforce.workloads.disruptionBudgets", index)

	maximum := int64(2147483647)

	if err := validatePlacementRange(path+".evictableReplicas", policy.EvictableReplicas, 0, &maximum); err != nil {
		return err
	}

	if policy.EvictableReplicas != nil && !slices.ContainsFunc(workloads.Targets, func(target rules.WorkloadValidationTarget) bool {
		return target == rules.ValidateDeployment || target == rules.ValidateStatefulSet || target == rules.ValidateReplicaSet || target == rules.ValidateReplicationController
	}) {
		return fmt.Errorf("%s.evictableReplicas requires a deployment, statefulset, replicaset or replicationcontroller target", path)
	}

	for _, value := range policy.UnhealthyPodEvictionPolicies {
		if value != policyv1.AlwaysAllow && value != policyv1.IfHealthyBudget {
			return fmt.Errorf("%s.unhealthyPodEvictionPolicies: unsupported policy %q", path, value)
		}
	}

	return nil
}
