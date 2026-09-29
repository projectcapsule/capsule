// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func placementDescription[T any](rule T) string {
	data, err := json.Marshal(rule)
	if err != nil {
		return fmt.Sprintf("%T", rule)
	}

	return string(data)
}

func evaluatePlacement[R any](pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody, name, reason string,
	values func(*corev1.Pod) []ruleengine.Value,
	extract func(rules.NamespaceRuleEnforceWorkloadsBody) []R,
	matches func(R, ruleengine.Value) (bool, error),
) (*ruleengine.Evaluation, error) {
	if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return body != nil && body.GetWorkloadTargets(rules.ValidatePod) && len(extract(body.Workloads)) > 0
	}) {
		return nil, nil
	}

	return evaluatePodRules(pod, bodies, podRuleSet[R]{
		Name: name, EventReason: reason, Values: values, EvaluateEmptyValues: true,
		Rules: func(body *rules.NamespaceRuleEnforceBody) []R {
			if body == nil || !body.GetWorkloadTargets(rules.ValidatePod) {
				return nil
			}

			return extract(body.Workloads)
		},
		Matches: func(rule R, value ruleengine.Value) (ruleengine.Match, error) {
			matched, err := matches(rule, value)

			return ruleengine.Match{Matched: matched}, err
		},
		RuleDescription: placementDescription[R],
	})
}

func (h *podRules) validateNodeSelectors(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	matcher := workloads.PlacementMatcher{Expressions: h.regexCache}

	return evaluatePlacement(pod, bodies, "nodeSelector", events.ReasonForbiddenPodNodeSelector,
		func(pod *corev1.Pod) []ruleengine.Value {
			keys := make([]string, 0, len(pod.Spec.NodeSelector))
			for key := range pod.Spec.NodeSelector {
				keys = append(keys, key)
			}

			sort.Strings(keys)

			values := make([]ruleengine.Value, 0, len(keys))
			for _, key := range keys {
				values = append(values, ruleengine.Value{Value: key + "=" + pod.Spec.NodeSelector[key], Path: fmt.Sprintf("spec.nodeSelector[%q]", key), Data: key})
			}

			return values
		},
		func(body rules.NamespaceRuleEnforceWorkloadsBody) []rules.WorkloadNodeSelectorMatch {
			return body.NodeSelector
		},
		func(rule rules.WorkloadNodeSelectorMatch, value ruleengine.Value) (bool, error) {
			key, ok := value.Data.(string)
			if !ok {
				return false, fmt.Errorf("invalid node selector value at %s", value.Path)
			}

			return matcher.NodeSelector(rule, key, pod.Spec.NodeSelector[key])
		})
}

func (h *podRules) validateTolerations(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	matcher := workloads.PlacementMatcher{Expressions: h.regexCache}

	return evaluatePlacement(pod, bodies, "toleration", events.ReasonForbiddenPodToleration,
		func(pod *corev1.Pod) []ruleengine.Value {
			values := make([]ruleengine.Value, 0, len(pod.Spec.Tolerations))
			for i, toleration := range pod.Spec.Tolerations {
				values = append(values, ruleengine.Value{Value: toleration.Key, Path: fmt.Sprintf("spec.tolerations[%d]", i), Data: toleration})
			}

			return values
		},
		func(body rules.NamespaceRuleEnforceWorkloadsBody) []rules.WorkloadTolerationMatch {
			return body.Tolerations
		},
		func(rule rules.WorkloadTolerationMatch, value ruleengine.Value) (bool, error) {
			toleration, ok := value.Data.(corev1.Toleration)
			if !ok {
				return false, fmt.Errorf("invalid toleration at %s", value.Path)
			}

			return matcher.Toleration(rule, toleration)
		})
}

func (h *podRules) validateTopologySpread(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	matcher := workloads.PlacementMatcher{Expressions: h.regexCache}

	return evaluatePlacement(pod, bodies, "topologySpreadConstraint", events.ReasonForbiddenPodTopologySpread,
		func(pod *corev1.Pod) []ruleengine.Value {
			values := make([]ruleengine.Value, 0, len(pod.Spec.TopologySpreadConstraints))
			for i, constraint := range pod.Spec.TopologySpreadConstraints {
				values = append(values, ruleengine.Value{Value: constraint.TopologyKey, Path: fmt.Sprintf("spec.topologySpreadConstraints[%d]", i), Data: constraint})
			}

			return values
		},
		func(body rules.NamespaceRuleEnforceWorkloadsBody) []rules.WorkloadTopologySpreadMatch {
			return body.TopologySpreadConstraints
		},
		func(rule rules.WorkloadTopologySpreadMatch, value ruleengine.Value) (bool, error) {
			constraint, ok := value.Data.(corev1.TopologySpreadConstraint)
			if !ok {
				return false, fmt.Errorf("invalid topology spread constraint at %s", value.Path)
			}

			return matcher.TopologySpread(rule, constraint, pod.Labels)
		})
}

func (h *podRules) validateAffinity(pod *corev1.Pod, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	matcher := workloads.PlacementMatcher{Expressions: h.regexCache}

	return evaluatePlacement(pod, bodies, "affinity", events.ReasonForbiddenPodAffinity, affinityValues,
		func(body rules.NamespaceRuleEnforceWorkloadsBody) []rules.WorkloadAffinityMatch { return body.Affinity },
		func(rule rules.WorkloadAffinityMatch, value ruleengine.Value) (bool, error) {
			term, ok := value.Data.(workloads.PlacementAffinityTerm)
			if !ok {
				return false, fmt.Errorf("invalid affinity term at %s", value.Path)
			}

			return matcher.Affinity(rule, term, pod.Namespace, pod.Labels)
		})
}

func affinityValues(pod *corev1.Pod) []ruleengine.Value {
	if pod.Spec.Affinity == nil {
		return nil
	}

	var values []ruleengine.Value

	add := func(term workloads.PlacementAffinityTerm, index int) {
		branch := "requiredDuringSchedulingIgnoredDuringExecution"
		if term.Mode == rules.PlacementAffinityPreferred {
			branch = "preferredDuringSchedulingIgnoredDuringExecution"
		}

		if term.Type == rules.PlacementNodeAffinity && term.Mode == rules.PlacementAffinityRequired {
			branch += ".nodeSelectorTerms"
		}

		values = append(values, ruleengine.Value{
			Value: string(term.Type) + "/" + string(term.Mode),
			Path:  fmt.Sprintf("spec.affinity.%s.%s[%d]", term.Type, branch, index), Data: term,
		})
	}

	affinity := pod.Spec.Affinity
	if node := affinity.NodeAffinity; node != nil {
		if node.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range node.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				add(workloads.PlacementAffinityTerm{Type: rules.PlacementNodeAffinity, Mode: rules.PlacementAffinityRequired, Node: &node.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i]}, i)
			}
		}

		for i := range node.PreferredDuringSchedulingIgnoredDuringExecution {
			term := &node.PreferredDuringSchedulingIgnoredDuringExecution[i]
			add(workloads.PlacementAffinityTerm{Type: rules.PlacementNodeAffinity, Mode: rules.PlacementAffinityPreferred, Weight: term.Weight, Node: &term.Preference}, i)
		}
	}

	addPodTerms := func(kind rules.PlacementAffinityType, required []corev1.PodAffinityTerm, preferred []corev1.WeightedPodAffinityTerm) {
		for i := range required {
			add(workloads.PlacementAffinityTerm{Type: kind, Mode: rules.PlacementAffinityRequired, Pod: &required[i]}, i)
		}

		for i := range preferred {
			add(workloads.PlacementAffinityTerm{Type: kind, Mode: rules.PlacementAffinityPreferred, Weight: preferred[i].Weight, Pod: &preferred[i].PodAffinityTerm}, i)
		}
	}
	if affinity.PodAffinity != nil {
		addPodTerms(rules.PlacementPodAffinity, affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	if affinity.PodAntiAffinity != nil {
		addPodTerms(rules.PlacementPodAntiAffinity, affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	return values
}
