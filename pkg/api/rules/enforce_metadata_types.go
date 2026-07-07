// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// MetadataRule defines metadata constraints for namespaced resources.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.labels) || has(self.annotations)",message="at least one of labels or annotations must be set"
type MetadataRule struct {
	runtime.VersionKinds `json:",inline"`

	// Labels defines metadata policies by label key.
	//
	// +optional
	Labels map[string]MetadataValueRule `json:"labels,omitempty"`

	// Annotations defines metadata policies by annotation key.
	//
	// +optional
	Annotations map[string]MetadataValueRule `json:"annotations,omitempty"`
}

// +kubebuilder:object:generate=true
type MetadataValueRule struct {
	// Required enforces that the metadata key must be present.
	//
	// This is mainly meaningful with action=allow. Deny and audit rules remain
	// value matchers and do not require missing metadata to exist.
	//
	// +optional
	// +kubebuilder:default:=false
	Required bool `json:"required,omitempty"`

	// Values defines allowed, denied, or audited values for the metadata key.
	//
	// If Required=true and Values is empty, only presence is enforced.
	//
	// +optional
	Values []runtime.ExpressionMatch `json:"values,omitempty"`
}
