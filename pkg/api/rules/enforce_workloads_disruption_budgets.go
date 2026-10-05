// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import policyv1 "k8s.io/api/policy/v1"

// WorkloadDisruptionBudgetRules constrains PDBs covering selected workloads.
// It does not require a PDB or guarantee live eviction availability.
// +kubebuilder:object:generate=true
type WorkloadDisruptionBudgetRules struct {
	// AllowOverlap permits multiple PDBs to select the same Pod or selected
	// controller template. False rejects overlap for allow/deny actions and
	// reports violations for audit. True permits overlap in an allow rule;
	// omitted does not participate in evaluation. Rule ordering is preserved.
	// Checks run on PDB selector changes and selected Pod/template label changes.
	// Conditions and audience apply to the admission request, including PDB writes.
	// Cross-resource admission reads are not an atomic transaction.
	// +optional
	AllowOverlap *bool `json:"allowOverlap,omitempty"`

	// EvictableReplicas bounds the configured number of evictable replicas of each
	// selected Deployment, StatefulSet, ReplicaSet or ReplicationController.
	// Uses desired spec.replicas and Kubernetes' upward percentage rounding;
	// zero replicas are exempt. Checks controller writes, /scale, and PDB writes.
	// Bounds are inclusive nonnegative integers; omitted bounds are unrestricted.
	// Allow requires compliance; deny/audit match violations, like resource constraints.
	// Other targets do not have a desired replica count and skip this property.
	// At least one supported controller target must be explicitly selected.
	// +optional
	EvictableReplicas *PlacementRange `json:"evictableReplicas,omitempty"`

	// UnhealthyPodEvictionPolicies matches the effective PDB policy. Omission in a
	// PDB means IfHealthyBudget. Allow accepts listed values; deny/audit match
	// listed values, like other enum lists. Empty imposes no restriction.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:items:Enum=IfHealthyBudget;AlwaysAllow
	// +listType=set
	// +optional
	UnhealthyPodEvictionPolicies []policyv1.UnhealthyPodEvictionPolicyType `json:"unhealthyPodEvictionPolicies,omitempty"`
}
