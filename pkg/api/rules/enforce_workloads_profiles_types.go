// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import "github.com/projectcapsule/capsule/pkg/api/runtime"

// SecurityProfileType selects a native seccomp or AppArmor profile type.
// +kubebuilder:validation:Enum=RuntimeDefault;Localhost;Unconfined
type SecurityProfileType string

const (
	SecurityProfileRuntimeDefault SecurityProfileType = "RuntimeDefault"
	SecurityProfileLocalhost      SecurityProfileType = "Localhost"
	SecurityProfileUnconfined     SecurityProfileType = "Unconfined"
)

// WorkloadSecurityProfileMatch matches a profile type and optionally its local
// name/path. Entries are alternatives; localhostProfiles constrains only Localhost.
// +kubebuilder:object:generate=true
type WorkloadSecurityProfileMatch struct {
	// Types selects profile types. Missing profiles never match.
	// +kubebuilder:validation:MinItems=1
	Types []SecurityProfileType `json:"types"`

	// LocalhostProfiles matches seccomp paths relative to the kubelet seccomp
	// directory, or loaded AppArmor profile names. Omitted permits any Localhost
	// profile. Requires Localhost in types. Expressions use exact, exp and negate.
	// +optional
	// +kubebuilder:validation:MinItems=1
	LocalhostProfiles []runtime.ExpressionMatch `json:"localhostProfiles,omitempty"`
}
