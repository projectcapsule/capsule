// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

const maxPlacementNodeTerms = 256

// MutatePodPlacement applies ordered, audience-filtered placement and security mutations. It
// never changes rule/cache-owned values. Call only for main-resource Pod CREATE.
func MutatePodPlacement(ctx context.Context, pod *corev1.Pod, bodies []*apirules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	if pod == nil || !slices.ContainsFunc(bodies, func(body *apirules.NamespaceRuleBodyNamespace) bool { return body != nil && len(body.Mutate) > 0 }) {
		return false, nil
	}

	before := podPlacement(pod).DeepCopy()

	var containers []workloadMutationContainer

	linux := pod.Spec.OS == nil || pod.Spec.OS.Name != corev1.Windows

	for i, body := range bodies {
		if body == nil {
			continue
		}

		for j := range body.Mutate {
			mutation := &body.Mutate[j]
			if (len(mutation.Workloads.Targets) > 0 || mutation.Workloads.ReadOnlyRootFilesystem != nil || mutation.Workloads.Registries.ImagePullPolicy != "") && !mutationAppliesToPod(pod, mutation.Workloads) {
				continue
			}

			matched, err := matchesMutationConditions(ctx, conditions, pod, mutation.Conditions)
			if err != nil {
				return false, fmt.Errorf("rules[%d].mutate[%d]: %w", i, j, err)
			}

			if !matched {
				continue
			}

			switch mutation.Action {
			case "", apirules.MutationActionMerge:
				if mutation.Workloads.GetWorkloadTargets(apirules.ValidatePod) {
					if err := mergePodPlacement(pod, &mutation.Workloads); err != nil {
						return false, fmt.Errorf("rules[%d].mutate[%d].workloads: %w", i, j, err)
					}
				}
			case apirules.MutationActionReplace:
				if mutation.Workloads.GetWorkloadTargets(apirules.ValidatePod) {
					replacePodPlacement(pod, &mutation.Workloads)
				}
			default:
				return false, fmt.Errorf("rules[%d].mutate[%d].action: unsupported action %q (expected merge or replace)", i, j, mutation.Action)
			}

			if hasContainerMutation(mutation.Workloads, linux) {
				if containers == nil {
					containers = workloadMutationContainers(pod, false, nil)
				}

				mutateContainers(containers, mutation.Workloads, linux)
			}
		}
	}

	return slices.ContainsFunc(containers, workloadMutationContainer.changed) || podPlacementChanged(before, podPlacement(pod)), nil
}

func podPlacementChanged(before, after *apirules.WorkloadMutation) bool {
	return before.Scheduler != after.Scheduler ||
		(before.SeccompProfile != after.SeccompProfile && !equality.Semantic.DeepEqual(before.SeccompProfile, after.SeccompProfile)) ||
		(before.AppArmorProfile != after.AppArmorProfile && !equality.Semantic.DeepEqual(before.AppArmorProfile, after.AppArmorProfile)) ||
		!equality.Semantic.DeepEqual(before.HostUsers, after.HostUsers) ||
		!equality.Semantic.DeepEqual(before.NodeSelector, after.NodeSelector) ||
		!equality.Semantic.DeepEqual(before.Tolerations, after.Tolerations) ||
		!equality.Semantic.DeepEqual(before.TopologySpreadConstraints, after.TopologySpreadConstraints) ||
		!equality.Semantic.DeepEqual(before.Affinity, after.Affinity)
}

func mutationAppliesToPod(pod *corev1.Pod, workload apirules.WorkloadMutation) bool {
	if workload.HasPodProperties() && workload.GetWorkloadTargets(apirules.ValidatePod) {
		return true
	}

	if !hasContainerMutation(workload, pod.Spec.OS == nil || pod.Spec.OS.Name != corev1.Windows) {
		return false
	}

	return (len(pod.Spec.Containers) > 0 && workload.GetWorkloadTargets(apirules.ValidateContainers)) ||
		(len(pod.Spec.InitContainers) > 0 && workload.GetWorkloadTargets(apirules.ValidateInitContainers)) ||
		(len(pod.Spec.EphemeralContainers) > 0 && workload.GetWorkloadTargets(apirules.ValidateEphemeralContainers))
}

// A shallow read-only snapshot keeps the caller's Pod from escaping to the CEL
// interface on the unconditional path. The CEL converter copies nested values.
func matchesMutationConditions(ctx context.Context, evaluator *ruleengine.ConditionEvaluator, pod *corev1.Pod, conditions []apirules.AdmissionCondition) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	snapshot := *pod

	evaluator.ResetObject()

	return evaluator.Matches(ctx, &snapshot, conditions)
}

func podPlacement(pod *corev1.Pod) *apirules.WorkloadMutation {
	placement := &apirules.WorkloadMutation{
		Scheduler:    pod.Spec.SchedulerName,
		HostUsers:    pod.Spec.HostUsers,
		NodeSelector: pod.Spec.NodeSelector, Tolerations: pod.Spec.Tolerations,
		TopologySpreadConstraints: pod.Spec.TopologySpreadConstraints, Affinity: pod.Spec.Affinity,
	}
	if pod.Spec.SecurityContext != nil {
		placement.SeccompProfile = pod.Spec.SecurityContext.SeccompProfile
		placement.AppArmorProfile = pod.Spec.SecurityContext.AppArmorProfile
	}

	return placement
}

func replacePodPlacement(pod *corev1.Pod, placement *apirules.WorkloadMutation) {
	mutatePodSecurityProfiles(pod, placement, true)

	desired := placement.DeepCopy()
	if desired.Scheduler != "" {
		pod.Spec.SchedulerName = desired.Scheduler
	}

	if desired.HostUsers != nil {
		pod.Spec.HostUsers = desired.HostUsers
	}

	if desired.NodeSelector != nil {
		pod.Spec.NodeSelector = desired.NodeSelector
	}

	if desired.Tolerations != nil {
		for i := range desired.Tolerations {
			if desired.Tolerations[i].Operator == "" {
				desired.Tolerations[i].Operator = corev1.TolerationOpEqual
			}
		}

		pod.Spec.Tolerations = desired.Tolerations
	}

	if desired.TopologySpreadConstraints != nil {
		pod.Spec.TopologySpreadConstraints = desired.TopologySpreadConstraints
	}

	if desired.Affinity != nil {
		pod.Spec.Affinity = desired.Affinity
	}
}

func mergePodPlacement(pod *corev1.Pod, placement *apirules.WorkloadMutation) error {
	mutatePodSecurityProfiles(pod, placement, false)

	if placement.Scheduler != "" && pod.Spec.SchedulerName == "" {
		pod.Spec.SchedulerName = placement.Scheduler
	}

	if placement.HostUsers != nil {
		pod.Spec.HostUsers = new(*placement.HostUsers)
	}

	for key, value := range placement.NodeSelector {
		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = make(map[string]string)
		}

		pod.Spec.NodeSelector[key] = value
	}

	for _, desired := range placement.Tolerations {
		desired := *desired.DeepCopy()
		if desired.Operator == "" {
			desired.Operator = corev1.TolerationOpEqual
		}

		upsertPlacement(&pod.Spec.Tolerations, desired, sameToleration)
	}

	for _, desired := range placement.TopologySpreadConstraints {
		upsertPlacement(&pod.Spec.TopologySpreadConstraints, *desired.DeepCopy(), sameSpreadConstraint)
	}

	if placement.Affinity != nil {
		if err := ensureAffinity(&pod.Spec.Affinity, placement.Affinity); err != nil {
			return fmt.Errorf("affinity: %w", err)
		}
	}

	return nil
}

// Upsert replaces all matching identities with one copy at the original position.
func upsertPlacement[T any](items *[]T, desired T, same func(T, T) bool) bool {
	first := -1
	changed := false

	for i := 0; i < len(*items); i++ {
		if !same((*items)[i], desired) {
			continue
		}

		if first >= 0 {
			*items = slices.Delete(*items, i, i+1)
			i--
			changed = true

			continue
		}

		first = i

		if !equality.Semantic.DeepEqual((*items)[i], desired) {
			(*items)[i] = desired
			changed = true
		}
	}

	if first < 0 {
		*items = append(*items, desired)

		return true
	}

	return changed
}

func sameToleration(a, b corev1.Toleration) bool {
	if a.Operator == "" {
		a.Operator = corev1.TolerationOpEqual
	}

	return a.Key == b.Key && a.Operator == b.Operator && a.Value == b.Value && a.Effect == b.Effect
}

func sameSpreadConstraint(a, b corev1.TopologySpreadConstraint) bool {
	return a.TopologyKey == b.TopologyKey && a.WhenUnsatisfiable == b.WhenUnsatisfiable
}

func ensureAffinity(current **corev1.Affinity, desired *corev1.Affinity) error {
	if *current == nil {
		*current = &corev1.Affinity{}
	}

	if desired.NodeAffinity != nil {
		if (*current).NodeAffinity == nil {
			(*current).NodeAffinity = &corev1.NodeAffinity{}
		}

		if err := ensureNodeAffinity((*current).NodeAffinity, desired.NodeAffinity); err != nil {
			return fmt.Errorf("nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution: %w", err)
		}
	}

	if desired.PodAffinity != nil {
		if (*current).PodAffinity == nil {
			(*current).PodAffinity = &corev1.PodAffinity{}
		}

		ensurePodAffinityTerms(&(*current).PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			&(*current).PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			desired.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			desired.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	if desired.PodAntiAffinity != nil {
		if (*current).PodAntiAffinity == nil {
			(*current).PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		ensurePodAffinityTerms(&(*current).PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			&(*current).PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			desired.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			desired.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}

	return nil
}

func ensureNodeAffinity(current, desired *corev1.NodeAffinity) error {
	if err := ensureRequiredNodeAffinity(&current.RequiredDuringSchedulingIgnoredDuringExecution, desired.RequiredDuringSchedulingIgnoredDuringExecution); err != nil {
		return err
	}

	for _, term := range desired.PreferredDuringSchedulingIgnoredDuringExecution {
		term.Preference = normalizeNodeTerm(term.Preference)
		upsertPlacement(&current.PreferredDuringSchedulingIgnoredDuringExecution, term, func(a, b corev1.PreferredSchedulingTerm) bool {
			return equality.Semantic.DeepEqual(normalizeNodeTerm(a.Preference), b.Preference)
		})
	}

	return nil
}

func ensureRequiredNodeAffinity(current **corev1.NodeSelector, desired *corev1.NodeSelector) error {
	if desired == nil {
		return nil
	}

	baseline := simplifyNodeTerms(desired.NodeSelectorTerms)
	if len(baseline) > maxPlacementNodeTerms {
		return fmt.Errorf("required node affinity exceeds %d alternatives", maxPlacementNodeTerms)
	}

	if len(baseline) == 0 {
		baseline = []corev1.NodeSelectorTerm{{}}
	}

	if *current == nil {
		*current = &corev1.NodeSelector{NodeSelectorTerms: baseline}

		return nil
	}

	terms, err := conjoinNodeTerms((*current).NodeSelectorTerms, baseline)
	if err != nil {
		return err
	}

	(*current).NodeSelectorTerms = terms

	return nil
}

func conjoinNodeTerms(existing, baseline []corev1.NodeSelectorTerm) ([]corev1.NodeSelectorTerm, error) {
	existing = simplifyNodeTerms(existing)
	baseline = simplifyNodeTerms(baseline)
	// An empty selector/term is false, not an unconstrained conjunction.
	if len(existing) == 0 || len(baseline) == 0 {
		return []corev1.NodeSelectorTerm{{}}, nil
	}

	alreadyRestricted := true

	for _, term := range existing {
		if !slices.ContainsFunc(baseline, func(required corev1.NodeSelectorTerm) bool { return nodeTermContains(term, required) }) {
			alreadyRestricted = false

			break
		}
	}

	if alreadyRestricted {
		return existing, nil
	}

	if len(existing) > maxPlacementNodeTerms/len(baseline) {
		return nil, fmt.Errorf("required node affinity would exceed %d alternatives", maxPlacementNodeTerms)
	}

	combined := make([]corev1.NodeSelectorTerm, 0, len(existing)*len(baseline))

	for _, left := range existing {
		for _, right := range baseline {
			combined = append(combined, corev1.NodeSelectorTerm{
				MatchExpressions: append(slices.Clone(left.MatchExpressions), right.MatchExpressions...),
				MatchFields:      append(slices.Clone(left.MatchFields), right.MatchFields...),
			})
		}
	}

	return simplifyNodeTerms(combined), nil
}

func simplifyNodeTerms(terms []corev1.NodeSelectorTerm) []corev1.NodeSelectorTerm {
	var out []corev1.NodeSelectorTerm

	for _, term := range terms {
		if len(term.MatchExpressions)+len(term.MatchFields) == 0 {
			continue
		}

		term = normalizeNodeTerm(term)

		if slices.ContainsFunc(out, func(other corev1.NodeSelectorTerm) bool { return nodeTermContains(term, other) }) {
			continue
		}

		out = slices.DeleteFunc(out, func(other corev1.NodeSelectorTerm) bool { return nodeTermContains(other, term) })
		out = append(out, term)
	}

	return out
}

func nodeTermContains(term, required corev1.NodeSelectorTerm) bool {
	return requirementsContain(term.MatchExpressions, required.MatchExpressions) && requirementsContain(term.MatchFields, required.MatchFields)
}

func requirementsContain(terms, required []corev1.NodeSelectorRequirement) bool {
	for _, requirement := range required {
		if !slices.ContainsFunc(terms, func(term corev1.NodeSelectorRequirement) bool { return equality.Semantic.DeepEqual(term, requirement) }) {
			return false
		}
	}

	return true
}

func normalizeNodeTerm(term corev1.NodeSelectorTerm) corev1.NodeSelectorTerm {
	term.MatchExpressions = normalizeNodeRequirements(term.MatchExpressions)
	term.MatchFields = normalizeNodeRequirements(term.MatchFields)

	return term
}

func normalizeNodeRequirements(requirements []corev1.NodeSelectorRequirement) []corev1.NodeSelectorRequirement {
	var out []corev1.NodeSelectorRequirement

	for _, requirement := range requirements {
		requirement.Values = slices.Clone(requirement.Values)
		slices.Sort(requirement.Values)

		requirement.Values = slices.Compact(requirement.Values)
		if !slices.ContainsFunc(out, func(other corev1.NodeSelectorRequirement) bool {
			return equality.Semantic.DeepEqual(other, requirement)
		}) {
			out = append(out, requirement)
		}
	}

	slices.SortFunc(out, func(a, b corev1.NodeSelectorRequirement) int {
		if a.Key != b.Key {
			if a.Key < b.Key {
				return -1
			}

			return 1
		}

		if a.Operator != b.Operator {
			if a.Operator < b.Operator {
				return -1
			}

			return 1
		}

		return slices.Compare(a.Values, b.Values)
	})

	return out
}

func ensurePodAffinityTerms(required *[]corev1.PodAffinityTerm, preferred *[]corev1.WeightedPodAffinityTerm, desiredRequired []corev1.PodAffinityTerm, desiredPreferred []corev1.WeightedPodAffinityTerm) {
	for _, term := range desiredRequired {
		term = normalizePodAffinityTerm(term)
		upsertPlacement(required, term, func(a, b corev1.PodAffinityTerm) bool {
			return equality.Semantic.DeepEqual(normalizePodAffinityTerm(a), b)
		})
	}

	for _, term := range desiredPreferred {
		term.PodAffinityTerm = normalizePodAffinityTerm(term.PodAffinityTerm)
		upsertPlacement(preferred, term, func(a, b corev1.WeightedPodAffinityTerm) bool {
			return equality.Semantic.DeepEqual(normalizePodAffinityTerm(a.PodAffinityTerm), b.PodAffinityTerm)
		})
	}
}

func normalizePodAffinityTerm(term corev1.PodAffinityTerm) corev1.PodAffinityTerm {
	term = *term.DeepCopy()
	slices.Sort(term.Namespaces)
	term.Namespaces = slices.Compact(term.Namespaces)
	slices.Sort(term.MatchLabelKeys)
	term.MatchLabelKeys = slices.Compact(term.MatchLabelKeys)
	slices.Sort(term.MismatchLabelKeys)
	term.MismatchLabelKeys = slices.Compact(term.MismatchLabelKeys)
	normalizeLabelSelector(term.LabelSelector)
	normalizeLabelSelector(term.NamespaceSelector)

	return term
}

func normalizeLabelSelector(selector *metav1.LabelSelector) {
	if selector == nil {
		return
	}

	for i := range selector.MatchExpressions {
		slices.Sort(selector.MatchExpressions[i].Values)
		selector.MatchExpressions[i].Values = slices.Compact(selector.MatchExpressions[i].Values)
	}

	slices.SortFunc(selector.MatchExpressions, func(a, b metav1.LabelSelectorRequirement) int {
		if a.Key != b.Key {
			if a.Key < b.Key {
				return -1
			}

			return 1
		}

		if a.Operator != b.Operator {
			if a.Operator < b.Operator {
				return -1
			}

			return 1
		}

		return slices.Compare(a.Values, b.Values)
	})
}
