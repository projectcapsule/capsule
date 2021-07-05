// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

// +kubebuilder:validation:Enum=cordoned;active
type tenantState string

const (
	TenantStateActive   tenantState = "active"
	TenantStateCordoned tenantState = "cordoned"
)

// TenantStatus defines the observed state of Tenant
type TenantStatus struct {
	//+kubebuilder:default=active
	State      tenantState `json:"state"`
	Size       uint        `json:"size"`
	Namespaces []string    `json:"namespaces,omitempty"`
}
