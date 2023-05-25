// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// GlobalTenantResourceSpec defines the desired state of GlobalTenantResource.
type GlobalTenantResourceSpec struct {
	// Defines the Tenant selector used target the tenants on which resources must be propagated.
	TenantSelector     metav1.LabelSelector `json:"tenantSelector,omitempty"`
	TenantResourceSpec `json:",inline"`
}

// GlobalTenantResourceStatus defines the observed state of GlobalTenantResource.
type GlobalTenantResourceStatus struct {
	// List of Tenants addressed by the GlobalTenantResource.
	SelectedTenants []string `json:"selectedTenants"`
	// List of the replicated resources for the given TenantResource.
	ProcessedItems ProcessedItems `json:"processedItems"`
}

type ProcessedItems []ObjectReferenceStatus

func (p *ProcessedItems) AsSet() sets.Set[string] {
	set := sets.New[string]()

	for _, i := range *p {
		set.Insert(i.String())
	}

	return set
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// GlobalTenantResource allows to propagate resource replications to a specific subset of Tenant resources.
type GlobalTenantResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlobalTenantResourceSpec   `json:"spec,omitempty"`
	Status GlobalTenantResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GlobalTenantResourceList contains a list of GlobalTenantResource.
type GlobalTenantResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalTenantResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlobalTenantResource{}, &GlobalTenantResourceList{})
}
