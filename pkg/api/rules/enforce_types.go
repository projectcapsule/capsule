// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceBody struct {
	// Declare the action being performed on the enforcement rule:
	// deny: On match, deny admission request
	// allow: On match, allowed admission request
	// audit: On match, audit (post event) of admission request
	//+kubebuilder:default:=deny
	Action ActionType `json:"action,omitempty"`

	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	Registries []OCIRegistry `json:"registries,omitempty"`
}
