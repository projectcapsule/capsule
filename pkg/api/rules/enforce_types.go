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

	// Enforcement for Workloads (Pods)
	Workloads NamespaceRuleEnforceWorkloadsBody `json:"workloads,omitempty"`

	// Enforcement for Services.
	// +optional
	Services NamespaceRuleEnforceServicesBody `json:"services,omitempty"`
}
