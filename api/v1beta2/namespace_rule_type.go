// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// +kubebuilder:object:generate=true
type NamespaceRule struct {
	// Enforce these properties via Rules
	NamespaceRuleBody `json:",inline"`

	// Select namespaces which are going to usese
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleBody struct {
	// Enforcement for given rule
	//+optional
	Enforce NamespaceRuleEnforceBody `json:"enforce,omitzero"`

	// Permissions for given rule
	//+optional
	Permissions NamespaceRulePermissionBody `json:"permissions,omitzero"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceBody struct {
	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	Registries []api.OCIRegistry `json:"registries,omitempty"`
}

// +kubebuilder:object:generate=true
type NamespaceRulePermissionBody struct {
	// Define Promotion Rules which distributed additional ClusterRoles across the Tenant
	// for promoted ServiceAccounts.
	Promotions []*NamespaceRulePromotionRule `json:"rules,omitempty"`
}

type NamespaceRulePromotionRule struct {
	// ClusterRoles granted to the promoted ServiceAccounts across the Tenant
	// kubebuilder:validation:Minimum=1
	ClusterRoles []string `json:"clusterRoles,omitempty"`

	// Match ServiceAccounts which are promoted which are granted these additional ClusterRoles
	// across the Tenant
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Select namespaces which this promotion will apply to (in which namespaces the rbac will be created)
	TargetNamespaceSelector *metav1.LabelSelector `json:"targetNamespaceSelector,omitempty"`
}
