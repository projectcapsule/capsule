// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// For future implementation where users might manage RuleStatus CRs themselves
// +kubebuilder:object:generate=true
type NamespaceRuleBodyNamespace struct {
	// Enforcement for given rule
	//+optional
	Enforce NamespaceRuleEnforceBody `json:"enforce,omitzero"`
}

// Rules Distributed via Tenants
// +kubebuilder:object:generate=true
type NamespaceRuleBodyTenant struct {
	NamespaceRuleBodyNamespace `json:",inline"`

	// Select namespaces which are going to be targeted with this rule
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// Permissions for given rule
	//+optional
	Permissions NamespaceRulePermissionBody `json:"permissions,omitzero"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceBody struct {
	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	Registries []OCIRegistry `json:"registries,omitempty"`
}

// +kubebuilder:object:generate=true
type NamespaceRulePermissionBody struct {
	// Define Promotion Rules which distributed additional ClusterRoles across the Tenant
	// for promoted ServiceAccounts.
	Promotions []*NamespaceRulePromotionRule `json:"rules,omitempty"`
}

// +kubebuilder:object:generate=true
type NamespaceRulePromotionRule struct {
	// ClusterRoles granted to the promoted ServiceAccounts across the Tenant
	// kubebuilder:validation:Minimum=1
	ClusterRoles []string `json:"clusterRoles,omitempty"`

	// Match ServiceAccounts which are promoted which are granted these additional ClusterRoles
	// across the Tenant
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}
