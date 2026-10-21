// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// NamespaceRuleEnforceStorageBody grants additional access to unlabeled volumes
// and matches the classes referenced by storage objects.
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

	// Targets selects the kinds whose class reference is matched against classes.
	// volumesnapshot matches VolumeSnapshot spec.volumeSnapshotClassName.
	// Omitted targets select every supported kind. Requires classes.
	// +optional
	// +listType=set
	Targets []StorageValidationTarget `json:"targets,omitempty"`

	// Classes matches the class referenced by each targeted object. Entries are
	// ORed. An allow or deny rule requires the targeted object to name a class
	// whenever the class would otherwise be defaulted after admission. Without
	// targets, classes apply to every supported kind.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Classes []StorageClassMatch `json:"classes,omitempty"`
}

// StorageValidationTarget names a kind whose class reference is validated.
// +kubebuilder:validation:Enum=volumesnapshot
type StorageValidationTarget string

const (
	StorageTargetVolumeSnapshot StorageValidationTarget = "volumesnapshot"
)

// StorageClassMatch matches a class by name, or by the labels of the class
// object. Exact, exp and the label selector are ORed. The rule admission
// validator requires at least one of them. A class object that does not exist
// never matches the label selector.
// +kubebuilder:object:generate=true
type StorageClassMatch struct {
	metav1.LabelSelector `json:",inline"`

	// Exact matches one of the provided class names exactly.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +optional
	Exact []string `json:"exact,omitempty"`

	// Exp matches the class name by regular expression.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Expression string `json:"exp,omitempty"`
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
