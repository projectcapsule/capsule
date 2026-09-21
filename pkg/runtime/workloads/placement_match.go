// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// PlacementMatcher uses the process-local expression cache supplied by admission.
type PlacementMatcher struct {
	Expressions runtime.ExpressionRegexMatcher
}

func (m PlacementMatcher) NodeSelector(rule rules.WorkloadNodeSelectorMatch, key, value string) (bool, error) {
	matched, err := m.expression(rule.Key, key)
	if err != nil || !matched {
		return matched, err
	}

	return m.expression(rule.Values, value)
}

func (m PlacementMatcher) Toleration(rule rules.WorkloadTolerationMatch, value corev1.Toleration) (bool, error) {
	operator := value.Operator
	if operator == "" {
		operator = corev1.TolerationOpEqual
	}

	if !placementContains(rule.Operators, operator) || !placementContains(rule.Effects, value.Effect) {
		return false, nil
	}

	if duration := rule.TolerationSeconds; duration != nil {
		if value.TolerationSeconds == nil {
			if duration.AllowUnlimited != nil && !*duration.AllowUnlimited {
				return false, nil
			}
		} else if !placementInRange(&duration.PlacementRange, *value.TolerationSeconds) {
			return false, nil
		}
	}

	return m.NodeSelector(rule.WorkloadNodeSelectorMatch, value.Key, value.Value)
}

func (m PlacementMatcher) TopologySpread(rule rules.WorkloadTopologySpreadMatch, value corev1.TopologySpreadConstraint, podLabels map[string]string) (bool, error) {
	minDomains := int64(1)
	if value.MinDomains != nil {
		minDomains = int64(*value.MinDomains)
	}

	nodeAffinityPolicy, nodeTaintsPolicy := corev1.NodeInclusionPolicyHonor, corev1.NodeInclusionPolicyIgnore
	if value.NodeAffinityPolicy != nil {
		nodeAffinityPolicy = *value.NodeAffinityPolicy
	}

	if value.NodeTaintsPolicy != nil {
		nodeTaintsPolicy = *value.NodeTaintsPolicy
	}

	if !placementContains(rule.WhenUnsatisfiable, value.WhenUnsatisfiable) ||
		!placementInRange(rule.MaxSkew, int64(value.MaxSkew)) ||
		!placementInRange(rule.MinDomains, minDomains) ||
		!placementContains(rule.NodeAffinityPolicy, nodeAffinityPolicy) ||
		!placementContains(rule.NodeTaintsPolicy, nodeTaintsPolicy) {
		return false, nil
	}

	matched, err := m.expression(rule.TopologyKey, value.TopologyKey)
	if err != nil || !matched {
		return matched, err
	}

	return m.LabelSelector(rule.LabelSelector, value.LabelSelector, value.MatchLabelKeys, nil, podLabels)
}

// LabelSelector checks matchLabels, matchExpressions and dynamic keys together.
// It never mutates a selector that may belong to a cache or an admission object.
func (m PlacementMatcher) LabelSelector(rule *rules.PlacementLabelSelectorMatch, value *metav1.LabelSelector, matchKeys, mismatchKeys []string, podLabels map[string]string) (bool, error) {
	if rule == nil {
		return true, nil
	}

	count := 0

	if value != nil {
		for key, label := range value.MatchLabels {
			count++

			matched, err := m.requirement(rule.Requirements, corev1.NodeSelectorRequirement{Key: key, Operator: corev1.NodeSelectorOpIn, Values: []string{label}})
			if err != nil || !matched {
				return matched, err
			}
		}

		for _, expression := range value.MatchExpressions {
			count++

			matched, err := m.requirement(rule.Requirements, corev1.NodeSelectorRequirement{Key: expression.Key, Operator: corev1.NodeSelectorOperator(expression.Operator), Values: expression.Values})
			if err != nil || !matched {
				return matched, err
			}
		}
	}

	for _, dynamic := range []struct {
		keys     []string
		operator corev1.NodeSelectorOperator
	}{
		{matchKeys, corev1.NodeSelectorOpIn}, {mismatchKeys, corev1.NodeSelectorOpNotIn},
	} {
		for _, key := range dynamic.keys {
			label, exists := podLabels[key]
			if !exists {
				continue
			}

			count++

			matched, err := m.requirement(rule.Requirements, corev1.NodeSelectorRequirement{Key: key, Operator: dynamic.operator, Values: []string{label}})
			if err != nil || !matched {
				return matched, err
			}
		}
	}

	return !rule.Required || count > 0, nil
}

// PlacementAffinityTerm retains term boundaries: constraints from different
// allow entries cannot accidentally be combined to permit an entire term.
type PlacementAffinityTerm struct {
	Type   rules.PlacementAffinityType
	Mode   rules.PlacementAffinityMode
	Weight int32
	Node   *corev1.NodeSelectorTerm
	Pod    *corev1.PodAffinityTerm
}

func (m PlacementMatcher) Affinity(rule rules.WorkloadAffinityMatch, value PlacementAffinityTerm, namespace string, podLabels map[string]string) (bool, error) {
	if !placementContains(rule.Types, value.Type) || !placementContains(rule.Modes, value.Mode) || !placementInRange(rule.Weight, int64(value.Weight)) {
		return false, nil
	}

	if value.Node != nil {
		return m.nodeAffinity(rule, value.Node)
	}

	if value.Pod == nil || len(rule.Requirements) > 0 || len(rule.FieldRequirements) > 0 {
		return false, nil
	}

	term := value.Pod
	if rule.NamespaceScope == rules.PlacementSameNamespace {
		if term.NamespaceSelector != nil {
			return false, nil
		}

		for _, termNamespace := range term.Namespaces {
			if termNamespace != namespace {
				return false, nil
			}
		}
	}

	for _, termNamespace := range term.Namespaces {
		matched, err := m.expression(rule.Namespaces, termNamespace)
		if err != nil || !matched {
			return matched, err
		}
	}

	matched, err := m.expression(rule.TopologyKey, term.TopologyKey)
	if err != nil || !matched {
		return matched, err
	}

	matched, err = m.LabelSelector(rule.NamespaceSelector, term.NamespaceSelector, nil, nil, nil)
	if err != nil || !matched {
		return matched, err
	}

	return m.LabelSelector(rule.LabelSelector, term.LabelSelector, term.MatchLabelKeys, term.MismatchLabelKeys, podLabels)
}

func (m PlacementMatcher) nodeAffinity(rule rules.WorkloadAffinityMatch, term *corev1.NodeSelectorTerm) (bool, error) {
	if rule.TopologyKey != nil || rule.NamespaceScope != "" || rule.Namespaces != nil || rule.NamespaceSelector != nil || rule.LabelSelector != nil {
		return false, nil
	}

	if len(rule.Requirements) > 0 && len(rule.FieldRequirements) == 0 && len(term.MatchFields) > 0 {
		return false, nil
	}

	if len(rule.FieldRequirements) > 0 && len(rule.Requirements) == 0 && len(term.MatchExpressions) > 0 {
		return false, nil
	}

	for _, requirement := range term.MatchExpressions {
		matched, err := m.requirement(rule.Requirements, requirement)
		if err != nil || !matched {
			return matched, err
		}
	}

	for _, requirement := range term.MatchFields {
		matched, err := m.requirement(rule.FieldRequirements, requirement)
		if err != nil || !matched {
			return matched, err
		}
	}

	return true, nil
}

func placementContains[T comparable](values []T, value T) bool {
	return len(values) == 0 || slices.Contains(values, value)
}

func placementInRange(bounds *rules.PlacementRange, value int64) bool {
	return bounds == nil || ((bounds.Min == nil || value >= *bounds.Min) && (bounds.Max == nil || value <= *bounds.Max))
}

func (m PlacementMatcher) expression(rule *rules.PlacementExpressionMatch, value string) (bool, error) {
	if rule == nil {
		return true, nil
	}

	return (*runtime.ExpressionMatch)(rule).MatchesWithExpressionMatcher(m.Expressions, value)
}

func (m PlacementMatcher) requirement(allowed []rules.PlacementRequirementMatch, value corev1.NodeSelectorRequirement) (bool, error) {
	if len(allowed) == 0 {
		return true, nil
	}

	for _, rule := range allowed {
		matched, err := m.matchRequirement(rule, value)
		if err != nil || matched {
			return matched, err
		}
	}

	return false, nil
}

func (m PlacementMatcher) matchRequirement(rule rules.PlacementRequirementMatch, value corev1.NodeSelectorRequirement) (bool, error) {
	if !placementContains(rule.Operators, value.Operator) {
		return false, nil
	}

	matched, err := m.expression(rule.Key, value.Key)
	if err != nil || !matched {
		return matched, err
	}

	for _, value := range value.Values {
		matched, err := m.expression(rule.Values, value)
		if err != nil || !matched {
			return matched, err
		}
	}

	return true, nil
}
