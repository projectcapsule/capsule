// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// +kubebuilder:object:generate=true
type NamespaceRulePermissionBody struct {
	// Bindings defines additional RoleBindings for namespaces selected by this rule.
	Bindings []rbac.AdditionalRoleBindingsSpec `json:"bindings,omitempty"`

	// Define Promotion Rules which distributed additional ClusterRoles across the Tenant
	// for promoted ServiceAccounts.
	Promotions []*NamespaceRulePromotionRule `json:"promotions,omitempty"`
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
