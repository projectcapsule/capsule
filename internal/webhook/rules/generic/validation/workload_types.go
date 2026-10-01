// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"slices"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func workloadTypeForGVK(gvk schema.GroupVersionKind) (rules.WorkloadValidationTarget, bool) {
	return rules.WorkloadTargetForGVK(gvk)
}

func hasWorkloadTypePolicy(gvk schema.GroupVersionKind, bodies []*rules.NamespaceRuleEnforceBody) bool {
	if _, supported := workloadTypeForGVK(gvk); !supported {
		return false
	}

	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return workloadKindRuleApplies(gvk, body)
	})
}

func workloadKindRuleApplies(gvk schema.GroupVersionKind, body *rules.NamespaceRuleEnforceBody) bool {
	if body == nil || !body.Workloads.TargetsOnly() {
		return false
	}

	// An allow-list must also evaluate unmatched kinds. Deny/audit conditions
	// need not run on another kind, whose object may have a different shape.
	if body.Action == rules.ActionTypeAllow {
		return true
	}

	return slices.ContainsFunc(body.Workloads.Targets, func(target rules.WorkloadValidationTarget) bool {
		gk, valid := target.GroupKind()

		return valid && gk == gvk.GroupKind()
	})
}

func (*genericRules) validateWorkloadTypes(
	_ genericObject,
	obj genericObject,
	gvk schema.GroupVersionKind,
	bodies []*rules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if obj == nil || !hasWorkloadTypePolicy(gvk, bodies) {
		return nil, nil
	}

	return evaluateGenericRules(obj, bodies, genericRuleSet[rules.WorkloadValidationTarget]{
		Name:        "workload type",
		EventReason: events.ReasonForbiddenWorkloadType,
		Values: func(genericObject) []ruleengine.Value {
			return []ruleengine.Value{{Value: gvk.Kind, Path: "kind"}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.WorkloadValidationTarget {
			if !body.Workloads.TargetsOnly() {
				return nil
			}

			return body.Workloads.Targets
		},
		Matches: func(workloadType rules.WorkloadValidationTarget, value ruleengine.Value) (ruleengine.Match, error) {
			gk, ok := workloadType.GroupKind()

			return ruleengine.Match{Matched: ok && gk.Kind == value.Value, MatchedValue: workloadType}, nil
		},
		RuleDescription:    func(workloadType rules.WorkloadValidationTarget) string { return string(workloadType) },
		AllowedDescription: "Allowed workload types",
	})
}
