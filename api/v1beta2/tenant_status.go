// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Cordoned;Active
type tenantState string

const (
	TenantStateActive   tenantState = "Active"
	TenantStateCordoned tenantState = "Cordoned"
)

// Returns the observed state of the Tenant.
type TenantStatus struct {
	// +kubebuilder:default=Active
	// The operational state of the Tenant. Possible values are "Active", "Cordoned".
	State tenantState `json:"state"`
	// How many namespaces are assigned to the Tenant.
	Size uint `json:"size"`
	// List of namespaces assigned to the Tenant.
	Namespaces []string `json:"namespaces,omitempty"`
	// Conditions represent the latest available observations of an object's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the status conditions of the object.
func (in Tenant) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the object.
func (in *Tenant) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}
