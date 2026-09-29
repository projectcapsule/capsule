// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import corev1 "k8s.io/api/core/v1"

// MutationAction selects merge or replacement of supplied workload properties.
type MutationAction string

const (
	MutationActionMerge   MutationAction = "merge"
	MutationActionReplace MutationAction = "replace"
)

// NamespaceRuleMutation applies typed mutations independently of enforce.action.
// Mutated values remain subject to all applicable enforcement rules.
// +kubebuilder:object:generate=true
type NamespaceRuleMutation struct {
	// Conditions gate this entire mutation entry and inspect the object after
	// preceding mutations. All conditions must be true; empty means apply.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Conditions []AdmissionCondition `json:"conditions,omitempty"`

	// Action chooses how explicitly supplied properties are applied.
	// Merge sets scalar values and map keys, upserts lists, and conjoins required affinity.
	// Replace replaces each supplied property in full: hostUsers, nodeSelector,
	// tolerations, topologySpreadConstraints, or affinity. Omitted properties are
	// retained. Supplying affinity replaces all its branches, including omitted ones.
	// +optional
	// +kubebuilder:default=merge
	// +kubebuilder:validation:Enum=merge;replace
	Action MutationAction `json:"action,omitempty"`

	// +optional
	Workloads WorkloadMutation `json:"workloads,omitempty"`
}

// WorkloadMutation contains typed native Pod values. Empty maps/lists are
// preserved so replace can clear a property; nil means the property is omitted.
// It applies only on Pod creation and never reconciles running Pods.
// On merge, later entries override hostUsers, matching map keys, tolerations and spread constraints.
// On merge, required affinity restrictions from applicable entries are ANDed.
// +kubebuilder:object:generate=true
type WorkloadMutation struct {
	// HostUsers sets spec.hostUsers on both merge and replace. False requests a
	// separate user namespace; true uses the host user namespace. Omitted or null
	// leaves the Pod value unchanged. Requires Kubernetes/runtime support.
	// +optional
	HostUsers *bool `json:"hostUsers,omitempty"`

	// NodeSelector sets configured keys on merge, or replaces the map on replace.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitzero"`

	// Tolerations merges by key, operator (default Equal), value and effect.
	// Changing any identity field adds another toleration, retaining the old one.
	// A matching toleration's duration is replaced; omitting it makes it unlimited.
	// Replace replaces the entire list, including entries with other identities.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitzero"`

	// TopologySpreadConstraints merges by topologyKey and whenUnsatisfiable.
	// The entire matching constraint is replaced, including its selector and
	// optional fields; other constraints remain. Replace replaces the entire list.
	// +optional
	// +listType=map
	// +listMapKey=topologyKey
	// +listMapKey=whenUnsatisfiable
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitzero"`

	// Affinity on merge conjoins required restrictions and upserts preferred terms.
	// Preferred terms match by their complete term, excluding weight and normalized
	// ordering. A different term is added; a matching term gets the supplied weight.
	// Required node affinity is distributed over existing OR alternatives, with
	// at most 256 resulting alternatives. Empty node selector terms match no nodes.
	// Replace replaces all affinity, including any branches omitted from the rule.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}
