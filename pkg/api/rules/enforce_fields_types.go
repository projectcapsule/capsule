// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// FieldRule defines value constraints for a declared field path on
// namespaced resources.
//
// Rules constrain values that exist: a path resolving to no value (or only
// empty values) is not evaluated, so field rules never make a field
// required. Note that server-side defaulting (DefaultStorageClass,
// LimitRanger, ...) runs before validating webhooks, so defaulted fields
// are already populated when rules are evaluated.
//
// +kubebuilder:object:generate=true
type FieldRule struct {
	runtime.VersionKinds `json:",inline"`

	// Path is a JSONPath expression into the object, written with a leading
	// dot and without surrounding curly braces (the same form used by
	// CustomQuota source paths), e.g.
	// ".spec.template.spec.containers[*].image", where "[*]" expands every
	// element of an array field into one checked value.
	//
	// Values resolved by the path must be scalars, and violations are
	// reported against the configured path, not concrete array indexes.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Path string `json:"path"`

	// Match defines allowed, denied, or audited values for the field,
	// depending on the enforcement action.
	//
	// +kubebuilder:validation:MinItems=1
	Match []runtime.ExpressionMatch `json:"match"`
}
