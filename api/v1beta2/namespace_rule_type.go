// Copyright 2020-2025 Project Capsule Authors
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
	// Enforcement Rules applied
	//+optional
	Enforce NamespaceRuleEnforceBody `json:"enforce,omitzero"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceBody struct {
	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	Registries []api.OCIRegistry `json:"registries,omitempty"`
}
