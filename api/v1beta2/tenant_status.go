// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/meta"
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
	// List of namespaces assigned to the Tenant. (Deprecated)
	Namespaces []string `json:"namespaces,omitempty"`
	// Tracks state for the namespaces associated with this tenant
	Spaces []*TenantStatusNamespaceItem `json:"spaces,omitempty"`
	// Tenant Condition
	Conditions meta.ConditionList `json:"conditions"`
}

type TenantStatusNamespaceItem struct {
	// Conditions
	Conditions meta.ConditionList `json:"conditions"`
	// Namespace Name
	Name string `json:"name"`
	// Namespace UID
	UID k8stypes.UID `json:"uid,omitempty"`
	// Managed Metadata
	Metadata *TenantStatusNamespaceMetadata `json:"metadata,omitempty"`
}

type TenantStatusNamespaceMetadata struct {
	// Managed Labels
	Labels map[string]string `json:"labels,omitempty"`
	// Managed Annotations
	Annotations map[string]string `json:"annotations,omitempty"`
}

func (ms *TenantStatus) GetInstance(stat *TenantStatusNamespaceItem) *TenantStatusNamespaceItem {
	for _, source := range ms.Spaces {
		if ms.instancequal(source, stat) {
			return source
		}
	}

	return nil
}

func (ms *TenantStatus) UpdateInstance(stat *TenantStatusNamespaceItem) {
	// Check if the tenant is already present in the status
	for i, source := range ms.Spaces {
		if ms.instancequal(source, stat) {
			ms.Spaces[i] = stat

			return
		}
	}

	ms.Spaces = append(ms.Spaces, stat)
}

func (ms *TenantStatus) RemoveInstance(stat *TenantStatusNamespaceItem) {
	// Filter out the datasource with given UID
	filter := []*TenantStatusNamespaceItem{}

	for _, source := range ms.Spaces {
		if !ms.instancequal(source, stat) {
			filter = append(filter, source)
		}
	}

	ms.Spaces = filter
}

func (ms *TenantStatus) instancequal(a, b *TenantStatusNamespaceItem) bool {
	return a.Name == b.Name
}
