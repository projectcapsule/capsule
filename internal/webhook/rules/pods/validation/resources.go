// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type workloadResourceLocation struct {
	target    apirules.WorkloadValidationTarget
	path      string
	resources *corev1.ResourceRequirements
}

type workloadResourceConstraint struct {
	action apirules.ActionType
	policy string
	value  *resource.Quantity
}

type workloadResourceField string

const (
	workloadResourceRequests workloadResourceField = "request"
	workloadResourceLimits   workloadResourceField = "limit"
)

func (h *podRules) validateResources(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if pod == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	out := &ruleengine.Evaluation{}

	for _, location := range workloadResourceLocations(pod, enforceBodies) {
		requests, limits := workloadResourceNames(enforceBodies, location.target)

		for _, name := range requests {
			evaluation, err := evaluateWorkloadResourceField(
				location,
				name,
				workloadResourceRequests,
				workloadResourceConstraints(enforceBodies, location.target, name, workloadResourceRequests),
			)
			if err != nil {
				return out, err
			}

			out.Append(evaluation)

			if evaluation != nil && evaluation.Blocking != nil {
				return out, nil
			}
		}

		for _, name := range limits {
			evaluation, err := evaluateWorkloadResourceField(
				location,
				name,
				workloadResourceLimits,
				workloadResourceConstraints(enforceBodies, location.target, name, workloadResourceLimits),
			)
			if err != nil {
				return out, err
			}

			out.Append(evaluation)

			if evaluation != nil && evaluation.Blocking != nil {
				return out, nil
			}
		}
	}

	return out, nil
}

func workloadResourceLocations(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) []workloadResourceLocation {
	out := make([]workloadResourceLocation, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+1)

	if hasWorkloadResourcePolicies(enforceBodies, apirules.ValidatePod) {
		resources := pod.Spec.Resources
		if resources == nil {
			resources = &corev1.ResourceRequirements{}
		}

		out = append(out, workloadResourceLocation{
			target:    apirules.ValidatePod,
			path:      "spec.resources",
			resources: resources,
		})
	}

	if hasWorkloadResourcePolicies(enforceBodies, apirules.ValidateInitContainers) {
		for i := range pod.Spec.InitContainers {
			out = append(out, workloadResourceLocation{
				target:    apirules.ValidateInitContainers,
				path:      fmt.Sprintf("spec.initContainers[%d].resources", i),
				resources: &pod.Spec.InitContainers[i].Resources,
			})
		}
	}

	if hasWorkloadResourcePolicies(enforceBodies, apirules.ValidateContainers) {
		for i := range pod.Spec.Containers {
			out = append(out, workloadResourceLocation{
				target:    apirules.ValidateContainers,
				path:      fmt.Sprintf("spec.containers[%d].resources", i),
				resources: &pod.Spec.Containers[i].Resources,
			})
		}
	}

	return out
}

func hasWorkloadResourcePolicies(
	bodies []*apirules.NamespaceRuleEnforceBody,
	target apirules.WorkloadValidationTarget,
) bool {
	for _, enforce := range bodies {
		if enforce == nil || enforce.Workloads.Resources == nil {
			continue
		}

		if validationResourcePoliciesTarget(enforce.Workloads, target) {
			return true
		}
	}

	return false
}

func validationResourcePoliciesTarget(
	workload apirules.NamespaceRuleEnforceWorkloadsBody,
	target apirules.WorkloadValidationTarget,
) bool {
	if len(workload.Targets) == 0 {
		return target == apirules.ValidateContainers || target == apirules.ValidateInitContainers
	}

	return slices.Contains(workload.Targets, target)
}

func workloadResourceNames(
	bodies []*apirules.NamespaceRuleEnforceBody,
	target apirules.WorkloadValidationTarget,
) (requests []corev1.ResourceName, limits []corev1.ResourceName) {
	requestSet := map[corev1.ResourceName]struct{}{}
	limitSet := map[corev1.ResourceName]struct{}{}

	for _, enforce := range bodies {
		if enforce == nil || enforce.Workloads.Resources == nil ||
			!validationResourcePoliciesTarget(enforce.Workloads, target) {
			continue
		}

		for name, policy := range enforce.Workloads.Resources.Requests {
			if policy.Policy == apirules.WorkloadResourceRequestPolicyRemove {
				requestSet[name] = struct{}{}
			}
		}

		for name, policy := range enforce.Workloads.Resources.Limits {
			switch policy.Policy {
			case apirules.WorkloadResourceLimitPolicyPreserve,
				apirules.WorkloadResourceLimitPolicyDefault:
			case apirules.WorkloadResourceLimitPolicyRemove,
				apirules.WorkloadResourceLimitPolicyMatchRequest,
				apirules.WorkloadResourceLimitPolicyRatio:
				limitSet[name] = struct{}{}
			}
		}
	}

	requests = sortedResourceNames(requestSet)
	limits = sortedResourceNames(limitSet)

	return requests, limits
}

func sortedResourceNames(set map[corev1.ResourceName]struct{}) []corev1.ResourceName {
	out := make([]corev1.ResourceName, 0, len(set))
	for name := range set {
		out = append(out, name)
	}

	slices.Sort(out)

	return out
}

func workloadResourceConstraints(
	bodies []*apirules.NamespaceRuleEnforceBody,
	target apirules.WorkloadValidationTarget,
	name corev1.ResourceName,
	field workloadResourceField,
) []workloadResourceConstraint {
	out := make([]workloadResourceConstraint, 0, 1)

	for _, enforce := range bodies {
		if enforce == nil || enforce.Workloads.Resources == nil ||
			!validationResourcePoliciesTarget(enforce.Workloads, target) {
			continue
		}

		action := enforce.Action.OrDefault()

		switch field {
		case workloadResourceRequests:
			policy, found := enforce.Workloads.Resources.Requests[name]
			if !found {
				continue
			}

			switch policy.Policy {
			case apirules.WorkloadResourceRequestPolicyPreserve,
				apirules.WorkloadResourceRequestPolicyDefault:
				out = out[:0]
			case apirules.WorkloadResourceRequestPolicyRemove:
				out = append(out, workloadResourceConstraint{action: action, policy: string(policy.Policy)})
			}
		case workloadResourceLimits:
			policy, found := enforce.Workloads.Resources.Limits[name]
			if !found {
				continue
			}

			switch policy.Policy {
			case apirules.WorkloadResourceLimitPolicyPreserve,
				apirules.WorkloadResourceLimitPolicyDefault:
				out = out[:0]
			case apirules.WorkloadResourceLimitPolicyRemove,
				apirules.WorkloadResourceLimitPolicyMatchRequest,
				apirules.WorkloadResourceLimitPolicyRatio:
				out = append(out, workloadResourceConstraint{
					action: action,
					policy: string(policy.Policy),
					value:  policy.Value,
				})
			}
		}
	}

	return out
}

func evaluateWorkloadResourceField(
	location workloadResourceLocation,
	name corev1.ResourceName,
	field workloadResourceField,
	constraints []workloadResourceConstraint,
) (*ruleengine.Evaluation, error) {
	if len(constraints) == 0 {
		return nil, nil
	}

	evaluation := &ruleengine.Evaluation{}
	hasAllow := false

	var lastDecision *ruleengine.Decision

	for _, constraint := range constraints {
		compliant, detail, err := workloadResourceCompliant(location.resources, name, field, constraint)
		if err != nil {
			return evaluation, err
		}

		switch constraint.action {
		case apirules.ActionTypeAllow:
			hasAllow = true

			if compliant {
				lastDecision = workloadResourceDecision(location, name, field, constraint, detail, true)
			}
		case apirules.ActionTypeDeny:
			if !compliant {
				lastDecision = workloadResourceDecision(location, name, field, constraint, detail, false)
			}
		case apirules.ActionTypeAudit:
			if !compliant {
				evaluation.Audits = append(
					evaluation.Audits,
					workloadResourceDecision(location, name, field, constraint, detail, false),
				)
			}
		default:
			return evaluation, fmt.Errorf("workload resources: unsupported rule action %q", constraint.action)
		}
	}

	if lastDecision != nil {
		evaluation.Final = lastDecision
		if lastDecision.Action == apirules.ActionTypeDeny {
			evaluation.Blocking = lastDecision
		}

		return evaluation, nil
	}

	if hasAllow {
		path := workloadResourcePath(location.path, field, name)
		evaluation.Blocking = &ruleengine.Decision{
			SetName:     "workload resource",
			EventReason: events.ReasonForbiddenPodResources,
			Action:      apirules.ActionTypeDeny,
			Value: ruleengine.Value{
				Value: workloadResourceValue(location.resources, name, field),
				Path:  path,
			},
			Message: fmt.Sprintf(
				"workload resource %s %q at %s does not satisfy any allowed resource policy",
				field,
				name,
				path,
			),
		}
	}

	return evaluation, nil
}

func workloadResourceCompliant(
	resources *corev1.ResourceRequirements,
	name corev1.ResourceName,
	field workloadResourceField,
	constraint workloadResourceConstraint,
) (bool, string, error) {
	if resources == nil {
		resources = &corev1.ResourceRequirements{}
	}

	switch field {
	case workloadResourceRequests:
		_, present := resources.Requests[name]

		return !present, fmt.Sprintf("request must be undefined for policy %s", constraint.policy), nil
	case workloadResourceLimits:
		limit, limitPresent := resources.Limits[name]

		switch apirules.WorkloadResourceLimitPolicyType(constraint.policy) {
		case apirules.WorkloadResourceLimitPolicyPreserve,
			apirules.WorkloadResourceLimitPolicyDefault:
			return false, "", fmt.Errorf("limit %q policy %q is not an enforcement constraint", name, constraint.policy)
		case apirules.WorkloadResourceLimitPolicyRemove:
			return !limitPresent, "limit must be undefined for policy Remove", nil
		case apirules.WorkloadResourceLimitPolicyMatchRequest:
			request, requestPresent := resources.Requests[name]
			if !requestPresent {
				return !limitPresent, "limit and request must both be undefined or equal for policy MatchRequest", nil
			}

			if !limitPresent {
				return false, fmt.Sprintf("limit is undefined while request is %s", request.String()), nil
			}

			return limit.Cmp(request) == 0,
				fmt.Sprintf("limit %s must equal request %s", limit.String(), request.String()),
				nil
		case apirules.WorkloadResourceLimitPolicyRatio:
			if constraint.value == nil {
				return false, "", fmt.Errorf("limit %q Ratio policy has no value", name)
			}

			request, requestPresent := resources.Requests[name]
			if !requestPresent || request.Sign() <= 0 {
				return false, "Ratio requires a request greater than zero", nil
			}

			if !limitPresent {
				return false, "limit is undefined", nil
			}

			maximum, err := workloads.LimitForRatio(name, request, *constraint.value)
			if err != nil {
				return false, "", err
			}

			return limit.Cmp(maximum) <= 0,
				fmt.Sprintf(
					"limit %s must not exceed %s (request %s x ratio %s)",
					limit.String(),
					maximum.String(),
					request.String(),
					constraint.value.String(),
				),
				nil
		default:
			return false, "", fmt.Errorf("limit %q has unsupported policy %q", name, constraint.policy)
		}
	default:
		return false, "", fmt.Errorf("unsupported workload resource field %q", field)
	}
}

func workloadResourceDecision(
	location workloadResourceLocation,
	name corev1.ResourceName,
	field workloadResourceField,
	constraint workloadResourceConstraint,
	detail string,
	compliant bool,
) *ruleengine.Decision {
	path := workloadResourcePath(location.path, field, name)

	verb := "violates"

	if compliant {
		verb = "satisfies"
	}

	message := fmt.Sprintf(
		"workload resource %s %q at %s %s policy %s",
		field,
		name,
		path,
		verb,
		constraint.policy,
	)

	if detail != "" {
		message += ": " + detail
	}

	return &ruleengine.Decision{
		SetName:     "workload resource",
		EventReason: events.ReasonForbiddenPodResources,
		Action:      constraint.action,
		Value: ruleengine.Value{
			Value: workloadResourceValue(location.resources, name, field),
			Path:  path,
		},
		MatchedRule: constraint.policy,
		MatchDetail: detail,
		Message:     message,
	}
}

func workloadResourcePath(
	base string,
	field workloadResourceField,
	name corev1.ResourceName,
) string {
	fieldName := "requests"
	if field == workloadResourceLimits {
		fieldName = "limits"
	}

	return fmt.Sprintf("%s.%s[%q]", base, fieldName, name)
}

func workloadResourceValue(
	resources *corev1.ResourceRequirements,
	name corev1.ResourceName,
	field workloadResourceField,
) string {
	if resources == nil {
		return "<undefined>"
	}

	list := resources.Requests
	if field == workloadResourceLimits {
		list = resources.Limits
	}

	value, found := list[name]
	if !found {
		return "<undefined>"
	}

	return value.String()
}
