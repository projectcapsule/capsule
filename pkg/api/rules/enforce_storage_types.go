// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// NamespaceRuleEnforceStorageBody grants additional access to unlabeled volumes.
// Existing tenant-owned volume access and cross-tenant ownership checks are
// unchanged. Selectors only read PV labels; they never mutate PVs or PVC selectors.
// +kubebuilder:object:generate=true
type NamespaceRuleEnforceStorageBody struct {
	// Volumes matches additional PersistentVolumes. Entries are ORed. Conditions
	// on the enclosing enforcement rule must also match. The last matching
	// allow/deny decides the exception; audit never grants access.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Volumes []PersistentVolumeMatch `json:"volumes,omitempty"`
}

// PersistentVolumeMatch matches labels on the referenced PersistentVolume.
// +kubebuilder:object:generate=true
type PersistentVolumeMatch struct {
	// Name identifies this match in admission and audit messages.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name,omitempty"`

	// Selector matches existing PV labels, independently of PVC.spec.selector.
	// An explicit empty selector matches all PVs. Capsule's tenant ownership
	// label cannot be overridden by a selector or an allow decision.
	// +kubebuilder:validation:Required
	Selector *metav1.LabelSelector `json:"selector"`
}
