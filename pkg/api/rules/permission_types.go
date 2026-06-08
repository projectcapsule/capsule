// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
