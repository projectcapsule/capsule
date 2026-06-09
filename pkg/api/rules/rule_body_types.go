// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// For future implementation where users might manage RuleStatus CRs themselves
// +kubebuilder:object:generate=true
type NamespaceRuleBodyNamespace struct {
	// Enforcement for given rule
	//+optional
	Enforce *NamespaceRuleEnforceBody `json:"enforce,omitzero"`
}

// Rules Distributed via Tenants
// +kubebuilder:object:generate=true
type NamespaceRuleBodyTenant struct {
	*NamespaceRuleBodyNamespace `json:",inline"`

	// Select namespaces which are going to be targeted with this rule
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// Permissions for given rule
	//+optional
	Permissions NamespaceRulePermissionBody `json:"permissions,omitempty"`
}
