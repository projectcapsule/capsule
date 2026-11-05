// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// WorkloadClassEnforcement contains matchers for the classes referenced by Pods.
// +kubebuilder:object:generate=true
type WorkloadClassEnforcement struct {
	// Priority matches spec.priorityClassName of selected Pods and Pod templates
	// by name, or by the labels of the PriorityClass. Entries, and the criteria
	// within each entry, are ORed. An applicable allow list rejects every other
	// class; audit never grants access. A Pod without a class is not matched:
	// Kubernetes assigns the cluster's global default class before admission, so
	// that name is matched like an explicit one. Exact and exp need no read; a
	// label selector reads the class, and a missing class never matches it.
	// Priority rules apply to the pod target and do not run on subresources.
	// An update that keeps the class is only checked again by class rules with
	// conditions, so other updates keep working after a rule change.
	// Legacy Tenant.spec.priorityClasses restrictions remain an additional boundary.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Priority []WorkloadClassMatch `json:"priority,omitempty"`
}

// WorkloadClassMatch reuses the shared class name and label matching semantics.
// +kubebuilder:object:generate=false
type WorkloadClassMatch = StorageClassMatch

// HasPolicies reports whether any class matcher is configured.
func (c WorkloadClassEnforcement) HasPolicies() bool {
	return len(c.Priority) > 0
}
