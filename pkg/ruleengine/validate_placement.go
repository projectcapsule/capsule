// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metavalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func validatePlacementRules(index int, workloads rules.NamespaceRuleEnforceWorkloadsBody) error {
	path := fmt.Sprintf("rules[%d].enforce.workloads", index)

	if err := workloads.VisitPlacementExpressions(func(name string, expression *runtime.ExpressionMatch) error {
		if expression == nil {
			return nil
		}

		if err := validateExpressionMatch(*expression, path+"."+name); err != nil {
			return err
		}

		if len(expression.Exact) == 0 && expression.Expression == "" {
			return fmt.Errorf("%s.%s: at least one of exact or exp must be set", path, name)
		}

		if slices.Contains(expression.Exact, "") {
			return fmt.Errorf("%s.%s.exact: empty strings are not supported; use exp: '^$'", path, name)
		}

		return nil
	}); err != nil {
		return err
	}

	for i, rule := range workloads.Tolerations {
		path := fmt.Sprintf("%s.tolerations[%d]", path, i)
		if err := placementEnum(path+".operators", rule.Operators, corev1.TolerationOpEqual, corev1.TolerationOpExists); err != nil {
			return err
		}

		if err := placementEnum(path+".effects", rule.Effects, "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute); err != nil {
			return err
		}

		if rule.TolerationSeconds != nil {
			if err := validatePlacementRange(path+".tolerationSeconds", &rule.TolerationSeconds.PlacementRange, 0, nil); err != nil {
				return err
			}
		}
	}

	for i, rule := range workloads.TopologySpreadConstraints {
		path := fmt.Sprintf("%s.topologySpreadConstraints[%d]", path, i)
		if err := placementEnum(path+".whenUnsatisfiable", rule.WhenUnsatisfiable, corev1.DoNotSchedule, corev1.ScheduleAnyway); err != nil {
			return err
		}

		if err := placementEnum(path+".nodeAffinityPolicy", rule.NodeAffinityPolicy, corev1.NodeInclusionPolicyHonor, corev1.NodeInclusionPolicyIgnore); err != nil {
			return err
		}

		if err := placementEnum(path+".nodeTaintsPolicy", rule.NodeTaintsPolicy, corev1.NodeInclusionPolicyHonor, corev1.NodeInclusionPolicyIgnore); err != nil {
			return err
		}

		if err := validatePlacementRange(path+".maxSkew", rule.MaxSkew, 1, nil); err != nil {
			return err
		}

		if err := validatePlacementRange(path+".minDomains", rule.MinDomains, 1, nil); err != nil {
			return err
		}

		if err := validatePlacementSelectorMatch(path+".labelSelector", rule.LabelSelector); err != nil {
			return err
		}
	}

	for i, rule := range workloads.Affinity {
		if err := validateAffinityMatch(fmt.Sprintf("%s.affinity[%d]", path, i), rule); err != nil {
			return err
		}
	}

	return nil
}

func validateAffinityMatch(path string, rule rules.WorkloadAffinityMatch) error {
	if err := placementEnum(path+".types", rule.Types, rules.PlacementNodeAffinity, rules.PlacementPodAffinity, rules.PlacementPodAntiAffinity); err != nil {
		return err
	}

	if err := placementEnum(path+".modes", rule.Modes, rules.PlacementAffinityRequired, rules.PlacementAffinityPreferred); err != nil {
		return err
	}

	if err := placementEnum(path+".namespaceScope", []rules.PlacementNamespaceScope{rule.NamespaceScope}, "", rules.PlacementSameNamespace, rules.PlacementAnyNamespace); err != nil {
		return err
	}

	if rule.Weight != nil && (len(rule.Modes) == 0 || slices.Contains(rule.Modes, rules.PlacementAffinityRequired)) {
		return fmt.Errorf("%s.weight requires modes: [preferred]", path)
	}

	maximum := int64(100)
	if err := validatePlacementRange(path+".weight", rule.Weight, 1, &maximum); err != nil {
		return err
	}

	nodeFields := len(rule.Requirements) > 0 || len(rule.FieldRequirements) > 0

	podFields := rule.TopologyKey != nil || rule.LabelSelector != nil || rule.NamespaceSelector != nil || rule.Namespaces != nil || rule.NamespaceScope != ""

	if nodeFields && podFields {
		return fmt.Errorf("%s: node and Pod affinity constraints cannot be combined in one matcher", path)
	}

	for _, kind := range rule.Types {
		if (kind == rules.PlacementNodeAffinity && podFields) || (kind != rules.PlacementNodeAffinity && nodeFields) {
			return fmt.Errorf("%s: constraints are incompatible with type %s", path, kind)
		}
	}

	if rule.NamespaceScope == rules.PlacementSameNamespace && rule.NamespaceSelector != nil {
		return fmt.Errorf("%s: SameNamespace does not permit namespaceSelector", path)
	}

	if err := validatePlacementRequirements(path+".requirements", rule.Requirements, true); err != nil {
		return err
	}

	if err := validatePlacementRequirements(path+".fieldRequirements", rule.FieldRequirements, true); err != nil {
		return err
	}

	if err := validatePlacementSelectorMatch(path+".labelSelector", rule.LabelSelector); err != nil {
		return err
	}

	return validatePlacementSelectorMatch(path+".namespaceSelector", rule.NamespaceSelector)
}

func validatePlacementRange(path string, bounds *rules.PlacementRange, minimum int64, maximum *int64) error {
	if bounds == nil {
		return nil
	}

	for _, bound := range []*int64{bounds.Min, bounds.Max} {
		if bound != nil && (*bound < minimum || (maximum != nil && *bound > *maximum)) {
			return fmt.Errorf("%s: bound %d is outside the supported range", path, *bound)
		}
	}

	if bounds.Min != nil && bounds.Max != nil && *bounds.Min > *bounds.Max {
		return fmt.Errorf("%s: min must not exceed max", path)
	}

	return nil
}

func validatePlacementSelectorMatch(path string, selector *rules.PlacementLabelSelectorMatch) error {
	if selector == nil {
		return nil
	}

	return validatePlacementRequirements(path+".requirements", selector.Requirements, false)
}

func validatePlacementRequirements(path string, requirements []rules.PlacementRequirementMatch, node bool) error {
	allowed := []corev1.NodeSelectorOperator{corev1.NodeSelectorOpIn, corev1.NodeSelectorOpNotIn, corev1.NodeSelectorOpExists, corev1.NodeSelectorOpDoesNotExist}
	if node {
		allowed = append(allowed, corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt)
	}

	for i, requirement := range requirements {
		if err := placementEnum(fmt.Sprintf("%s[%d].operators", path, i), requirement.Operators, allowed...); err != nil {
			return err
		}
	}

	return nil
}

func placementEnum[T ~string](path string, values []T, allowed ...T) error {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%s: unsupported value %q", path, value)
		}
	}

	return nil
}

func validateMutations(index int, mutations []rules.NamespaceRuleMutation) error {
	if len(mutations) > 64 {
		return fmt.Errorf("rules[%d].mutate: at most 64 entries are supported", index)
	}

	for i, mutation := range mutations {
		path := fmt.Sprintf("rules[%d].mutate[%d]", index, i)
		if mutation.Action != "" && mutation.Action != rules.MutationActionMerge && mutation.Action != rules.MutationActionReplace {
			return fmt.Errorf("%s.action: unsupported mutation action %q", path, mutation.Action)
		}

		if err := validateMutationPlacement(path+".workloads", mutation.Workloads); err != nil {
			return err
		}
	}

	return nil
}

func validateMutationPlacement(path string, workload rules.WorkloadMutation) error {
	podProperties := workload.HasPodProperties()
	if !podProperties && workload.ReadOnlyRootFilesystem == nil && workload.Registries.ImagePullPolicy == "" {
		return fmt.Errorf("%s: at least one workload mutation property must be supplied", path)
	}

	if err := validateMutationTargets(path, workload, podProperties); err != nil {
		return err
	}

	switch workload.Registries.ImagePullPolicy {
	case "", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		return fmt.Errorf("%s.registries.imagePullPolicy: unsupported pull policy %q", path, workload.Registries.ImagePullPolicy)
	}

	if err := validateSecurityProfileMutation(path, workload); err != nil {
		return err
	}

	if workload.Scheduler != "" && strings.TrimSpace(workload.Scheduler) == "" {
		return fmt.Errorf("%s.scheduler: scheduler name must not be blank", path)
	}

	for key, value := range workload.NodeSelector {
		if err := placementLabel(path+".nodeSelector", key, value); err != nil {
			return err
		}
	}

	for i, toleration := range workload.Tolerations {
		if err := validateEnsuredToleration(fmt.Sprintf("%s.tolerations[%d]", path, i), toleration); err != nil {
			return err
		}
	}

	seen := make(map[string]struct{})

	for i, constraint := range workload.TopologySpreadConstraints {
		path := fmt.Sprintf("%s.topologySpreadConstraints[%d]", path, i)

		identity := constraint.TopologyKey + "\x00" + string(constraint.WhenUnsatisfiable)

		if _, found := seen[identity]; found {
			return fmt.Errorf("%s: duplicate topologyKey/whenUnsatisfiable", path)
		}

		seen[identity] = struct{}{}

		if err := validateEnsuredSpread(path, constraint); err != nil {
			return err
		}
	}

	return validateEnsuredAffinity(path+".affinity", workload.Affinity)
}

func placementLabel(path, key, value string) error {
	if !strings.Contains(key, "{{") {
		if errors := validation.IsQualifiedName(key); len(errors) > 0 {
			return fmt.Errorf("%s: invalid label key %q: %s", path, key, strings.Join(errors, "; "))
		}
	}

	if !strings.Contains(value, "{{") {
		if errors := validation.IsValidLabelValue(value); len(errors) > 0 {
			return fmt.Errorf("%s: invalid label value %q: %s", path, value, strings.Join(errors, "; "))
		}
	}

	return nil
}

func validateEnsuredToleration(path string, value corev1.Toleration) error {
	if value.Key != "" {
		if err := placementLabel(path, value.Key, value.Value); err != nil {
			return err
		}
	}

	if err := placementEnum(path+".operator", []corev1.TolerationOperator{value.Operator}, "", corev1.TolerationOpEqual, corev1.TolerationOpExists); err != nil {
		return err
	}

	if err := placementEnum(path+".effect", []corev1.TaintEffect{value.Effect}, "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute); err != nil {
		return err
	}

	if value.Key == "" && value.Operator != corev1.TolerationOpExists {
		return fmt.Errorf("%s: an empty key requires operator Exists", path)
	}

	if value.Operator == corev1.TolerationOpExists && value.Value != "" {
		return fmt.Errorf("%s: operator Exists requires an empty value", path)
	}

	if value.TolerationSeconds != nil && (value.Effect != corev1.TaintEffectNoExecute || *value.TolerationSeconds < 0) {
		return fmt.Errorf("%s: tolerationSeconds requires NoExecute and a nonnegative duration", path)
	}

	return nil
}

func validateEnsuredSpread(path string, value corev1.TopologySpreadConstraint) error {
	if err := placementLabel(path+".topologyKey", value.TopologyKey, ""); err != nil {
		return err
	}

	if err := placementEnum(path+".whenUnsatisfiable", []corev1.UnsatisfiableConstraintAction{value.WhenUnsatisfiable}, corev1.DoNotSchedule, corev1.ScheduleAnyway); err != nil {
		return err
	}

	if value.MaxSkew < 1 {
		return fmt.Errorf("%s.maxSkew must be positive", path)
	}

	if value.MinDomains != nil && (*value.MinDomains < 1 || value.WhenUnsatisfiable != corev1.DoNotSchedule) {
		return fmt.Errorf("%s.minDomains requires a positive value and DoNotSchedule", path)
	}

	for _, policy := range []*corev1.NodeInclusionPolicy{value.NodeAffinityPolicy, value.NodeTaintsPolicy} {
		if policy != nil {
			if err := placementEnum(path+".nodeInclusionPolicy", []corev1.NodeInclusionPolicy{*policy}, corev1.NodeInclusionPolicyHonor, corev1.NodeInclusionPolicyIgnore); err != nil {
				return err
			}
		}
	}

	if err := validateNativePlacementSelector(path+".labelSelector", value.LabelSelector); err != nil {
		return err
	}

	return validateDynamicPlacementKeys(path, value.LabelSelector, value.MatchLabelKeys, nil)
}

func validateNativePlacementSelector(path string, selector *metav1.LabelSelector) error {
	if selector == nil {
		return nil
	}
	// Templates are validated again after rendering into the namespace RuleStatus.
	for key, value := range selector.MatchLabels {
		if strings.Contains(key, "{{") || strings.Contains(value, "{{") {
			return nil
		}
	}

	for _, requirement := range selector.MatchExpressions {
		if strings.Contains(requirement.Key, "{{") {
			return nil
		}

		for _, value := range requirement.Values {
			if strings.Contains(value, "{{") {
				return nil
			}
		}
	}

	if errors := metavalidation.ValidateLabelSelector(selector, metavalidation.LabelSelectorValidationOptions{}, field.NewPath(path)); len(errors) > 0 {
		return errors.ToAggregate()
	}

	return nil
}

func validateDynamicPlacementKeys(path string, selector *metav1.LabelSelector, matchKeys, mismatchKeys []string) error {
	if len(matchKeys)+len(mismatchKeys) > 0 && selector == nil {
		return fmt.Errorf("%s: dynamic label keys require labelSelector", path)
	}

	seen := make(map[string]struct{})

	if selector != nil {
		for key := range selector.MatchLabels {
			seen[key] = struct{}{}
		}

		for _, expression := range selector.MatchExpressions {
			seen[expression.Key] = struct{}{}
		}
	}

	for _, keys := range [][]string{matchKeys, mismatchKeys} {
		for _, key := range keys {
			if err := placementLabel(path+".dynamicLabelKeys", key, ""); err != nil {
				return err
			}

			if _, found := seen[key]; found {
				return fmt.Errorf("%s: duplicate or overlapping dynamic label key %q", path, key)
			}

			seen[key] = struct{}{}
		}
	}

	return nil
}

func validateEnsuredAffinity(path string, affinity *corev1.Affinity) error {
	if affinity == nil {
		return nil
	}

	if err := validateEnsuredNodeAffinity(path, affinity.NodeAffinity); err != nil {
		return err
	}

	if affinity.PodAffinity != nil {
		if err := validateNativePodAffinity(path+".podAffinity", affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution); err != nil {
			return err
		}
	}

	if affinity.PodAntiAffinity != nil {
		if err := validateNativePodAffinity(path+".podAntiAffinity", affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution); err != nil {
			return err
		}
	}

	return nil
}

func validateNativeNodeSelector(path string, selector *corev1.NodeSelector) error {
	if len(selector.NodeSelectorTerms) == 0 || len(selector.NodeSelectorTerms) > 256 {
		return fmt.Errorf("%s: nodeSelectorTerms must contain 1 to 256 alternatives", path)
	}

	for _, term := range selector.NodeSelectorTerms {
		for _, requirement := range term.MatchExpressions {
			if strings.Contains(requirement.Key, "{{") {
				return nil
			}

			for _, value := range requirement.Values {
				if strings.Contains(value, "{{") {
					return nil
				}
			}
		}
	}

	if _, err := nodeaffinity.NewNodeSelector(selector); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	return nil
}

func validateNativePodAffinity(path string, required []corev1.PodAffinityTerm, preferred []corev1.WeightedPodAffinityTerm) error {
	for i, term := range required {
		if err := validateNativePodAffinityTerm(fmt.Sprintf("%s.requiredDuringSchedulingIgnoredDuringExecution[%d]", path, i), term); err != nil {
			return err
		}
	}

	for i, term := range preferred {
		path := fmt.Sprintf("%s.preferredDuringSchedulingIgnoredDuringExecution[%d]", path, i)
		if term.Weight < 1 || term.Weight > 100 {
			return fmt.Errorf("%s.weight must be between 1 and 100", path)
		}

		if err := validateNativePodAffinityTerm(path, term.PodAffinityTerm); err != nil {
			return err
		}
	}

	return nil
}

func validateNativePodAffinityTerm(path string, term corev1.PodAffinityTerm) error {
	if err := placementLabel(path+".topologyKey", term.TopologyKey, ""); err != nil {
		return err
	}

	if err := validateNativePlacementSelector(path+".labelSelector", term.LabelSelector); err != nil {
		return err
	}

	if err := validateNativePlacementSelector(path+".namespaceSelector", term.NamespaceSelector); err != nil {
		return err
	}

	for _, namespace := range term.Namespaces {
		if !strings.Contains(namespace, "{{") {
			if errors := validation.IsDNS1123Label(namespace); len(errors) > 0 {
				return fmt.Errorf("%s.namespaces: %s", path, strings.Join(errors, "; "))
			}
		}
	}

	return validateDynamicPlacementKeys(path, term.LabelSelector, term.MatchLabelKeys, term.MismatchLabelKeys)
}

func validateEnsuredNodeAffinity(path string, node *corev1.NodeAffinity) error {
	if node == nil {
		return nil
	}

	if node.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		if err := validateNativeNodeSelector(path+".nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution", node.RequiredDuringSchedulingIgnoredDuringExecution); err != nil {
			return err
		}
	}

	for i, term := range node.PreferredDuringSchedulingIgnoredDuringExecution {
		path := fmt.Sprintf("%s.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[%d]", path, i)
		if term.Weight < 1 || term.Weight > 100 {
			return fmt.Errorf("%s.weight must be between 1 and 100", path)
		}

		if err := validateNativeNodeSelector(path, &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{term.Preference}}); err != nil {
			return err
		}
	}

	return nil
}
