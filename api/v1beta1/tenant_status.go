// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

// +kubebuilder:validation:Enum=Cordoned;Active
type tenantState string

const (
	TenantStateActive   tenantState = "Active"
	TenantStateCordoned tenantState = "Cordoned"
)

// Returns the observed state of the Tenant.
type TenantStatus struct {
	//+kubebuilder:default=Active
	// The operational state of the Tenant. Possible values are "Active", "Cordoned".
	State tenantState `json:"state"`
	// How many namespaces are assigned to the Tenant.
	Size uint `json:"size"`
	// List of namespaces assigned to the Tenant.
	Namespaces []string `json:"namespaces,omitempty"`
}
