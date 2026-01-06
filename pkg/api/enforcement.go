// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true
type EnforcementSpec struct {
	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	Registries []OCIRegistry `json:"registries,omitempty"`
}
