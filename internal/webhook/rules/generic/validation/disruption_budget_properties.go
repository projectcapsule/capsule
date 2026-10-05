// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"slices"
	"strconv"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func budgetSettings(body *rules.NamespaceRuleEnforceBody) *rules.WorkloadDisruptionBudgetRules {
	if body == nil {
		return nil
	}

	return body.Workloads.DisruptionBudgets
}

func hasEvictableBounds(policy *rules.WorkloadDisruptionBudgetRules) bool {
	return policy != nil && policy.EvictableReplicas != nil && (policy.EvictableReplicas.Min != nil || policy.EvictableReplicas.Max != nil)
}

func hasBudgetConstraints(body *rules.NamespaceRuleEnforceBody) bool {
	policy := budgetSettings(body)

	return policy != nil && ((policy.AllowOverlap != nil && !*policy.AllowOverlap) || hasEvictableBounds(policy) || len(policy.UnhealthyPodEvictionPolicies) > 0)
}

func hasOverlapConstraints(bodies []*rules.NamespaceRuleEnforceBody) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return budgetPolicy(body) != nil && !*budgetPolicy(body)
	})
}

func hasBudgetProperties(bodies []*rules.NamespaceRuleEnforceBody) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		policy := budgetSettings(body)

		return policy != nil && (hasEvictableBounds(policy) || len(policy.UnhealthyPodEvictionPolicies) > 0)
	})
}

func hasBudgetPropertiesForKind(bodies []*rules.NamespaceRuleEnforceBody, gvk schema.GroupVersionKind) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		policy := budgetSettings(body)

		return policy != nil && budgetTargets(body.Workloads, gvk) && (len(policy.UnhealthyPodEvictionPolicies) > 0 || (hasEvictableBounds(policy) && budgetScalable(gvk)))
	})
}

func budgetConstraintApplies(policy *rules.WorkloadDisruptionBudgetRules, gvk schema.GroupVersionKind) bool {
	return policy != nil && (policy.AllowOverlap != nil || len(policy.UnhealthyPodEvictionPolicies) > 0 || (hasEvictableBounds(policy) && budgetScalable(gvk)))
}

func budgetScalable(gvk schema.GroupVersionKind) bool {
	return (gvk.Group == "apps" && (gvk.Kind == "Deployment" || gvk.Kind == "StatefulSet" || gvk.Kind == "ReplicaSet")) || (gvk.Group == "" && gvk.Kind == "ReplicationController")
}

func evaluateBudgetPolicies(workload budgetWorkload, names []string, spec *policyv1.PodDisruptionBudgetSpec, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	if len(names) > 1 {
		return evaluateBudgetOverlap(workload, names, bodies)
	}

	out := &ruleengine.Evaluation{}
	if spec == nil || !hasBudgetPropertiesForKind(bodies, workload.gvk) {
		return out, nil
	}

	if workload.replicas != nil && *workload.replicas > 0 && slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return hasEvictableBounds(budgetSettings(body)) && budgetTargets(body.Workloads, workload.gvk)
	}) {
		count, err := configuredEvictableReplicas(spec, *workload.replicas)
		if err != nil {
			return nil, fmt.Errorf("PDB %s: %w", names[0], err)
		}

		evaluation, err := evaluateBudgetReplicas(workload, names[0], count, bodies)
		if err != nil {
			return nil, err
		}

		out.Append(evaluation)

		if out.Blocking != nil {
			return out, nil
		}
	}

	policy := policyv1.IfHealthyBudget
	if spec.UnhealthyPodEvictionPolicy != nil {
		policy = *spec.UnhealthyPodEvictionPolicy
	}

	evaluation, err := ruleengine.EvaluateEnforce(workload, bodies, ruleengine.Set[policyv1.UnhealthyPodEvictionPolicyType, budgetWorkload]{
		Name: "PDB unhealthy pod eviction policy", EventReason: events.ReasonForbiddenDisruptionBudget,
		Values: func(budgetWorkload) []ruleengine.Value {
			return []ruleengine.Value{{Value: string(policy), Path: budgetPropertyPath(workload, names[0])}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []policyv1.UnhealthyPodEvictionPolicyType {
			if budgetSettings(body) == nil || !budgetTargets(body.Workloads, workload.gvk) {
				return nil
			}

			return body.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies
		},
		Matches: func(allowed policyv1.UnhealthyPodEvictionPolicyType, value ruleengine.Value) (ruleengine.Match, error) {
			return ruleengine.Match{Matched: string(allowed) == value.Value}, nil
		},
		RuleDescription: func(allowed policyv1.UnhealthyPodEvictionPolicyType) string { return string(allowed) },
	})
	out.Append(evaluation)

	return out, err
}

func budgetPropertyPath(workload budgetWorkload, name string) string {
	return "PodDisruptionBudget/" + name + " selecting " + workload.gvk.Kind + "/" + workload.name
}

type budgetReplicaConstraint struct {
	bounds *rules.PlacementRange
	action rules.ActionType
}

func evaluateBudgetReplicas(workload budgetWorkload, name string, count int64, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	return ruleengine.EvaluateEnforce(workload, bodies, ruleengine.Set[budgetReplicaConstraint, budgetWorkload]{
		Name: "PDB evictable replicas", EventReason: events.ReasonForbiddenDisruptionBudget,
		Values: func(budgetWorkload) []ruleengine.Value {
			return []ruleengine.Value{{Value: strconv.FormatInt(count, 10), Path: budgetPropertyPath(workload, name)}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []budgetReplicaConstraint {
			if !hasEvictableBounds(budgetSettings(body)) || !budgetTargets(body.Workloads, workload.gvk) {
				return nil
			}

			return []budgetReplicaConstraint{{bounds: body.Workloads.DisruptionBudgets.EvictableReplicas, action: body.Action.OrDefault()}}
		},
		Matches: func(constraint budgetReplicaConstraint, _ ruleengine.Value) (ruleengine.Match, error) {
			bounds := constraint.bounds

			matched := (bounds.Min == nil || count >= *bounds.Min) && (bounds.Max == nil || count <= *bounds.Max)
			if constraint.action != rules.ActionTypeAllow {
				matched = !matched
			}

			return ruleengine.Match{Matched: matched}, nil
		},
		RuleDescription: func(constraint budgetReplicaConstraint) string {
			parts := "evictableReplicas"
			if constraint.bounds.Min != nil {
				parts += fmt.Sprintf(" min=%d", *constraint.bounds.Min)
			}

			if constraint.bounds.Max != nil {
				parts += fmt.Sprintf(" max=%d", *constraint.bounds.Max)
			}

			return parts
		},
	})
}

// This is a configuration check for each controller at its desired scale, not a
// prediction based on status.currentHealthy or status.disruptionsAllowed.
func configuredEvictableReplicas(spec *policyv1.PodDisruptionBudgetSpec, replicas int64) (int64, error) {
	if replicas < 0 || replicas > 2147483647 {
		return 0, fmt.Errorf("invalid desired replicas %d", replicas)
	}

	if spec.MinAvailable == nil && spec.MaxUnavailable == nil {
		// Kubernetes accepts this spec but leaves expectedPods and
		// disruptionsAllowed at zero. It is not an unrestricted budget.
		return 0, nil
	}

	if spec.MinAvailable != nil && spec.MaxUnavailable != nil {
		return 0, fmt.Errorf("minAvailable and maxUnavailable cannot both be set")
	}

	value := spec.MaxUnavailable
	if value == nil {
		value = spec.MinAvailable
	}

	count, err := intstr.GetScaledValueFromIntOrPercent(value, int(replicas), true)
	if err != nil {
		return 0, fmt.Errorf("invalid disruption budget: %w", err)
	}

	if count < 0 {
		return 0, fmt.Errorf("negative disruption budget")
	}

	allowed := int64(count)
	if spec.MinAvailable != nil {
		allowed = replicas - allowed
	}

	return min(replicas, max(int64(0), allowed)), nil
}
