// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// GlobalTenantResourceSpec defines the desired state of GlobalTenantResource.
type GlobalTenantResourceSpec struct {
	TenantResourceSpec `json:",inline"`

	// Resource Scope, Can either be
	// - Tenant: Create Resources for each tenant  in selected Tenants
	// - Namespace: Create Resources for each namespace in selected Tenants
	// +kubebuilder:default:=Namespace
	Scope api.ResourceScope `json:"scope"`
	// Defines the Tenant selector used target the tenants on which resources must be propagated.
	// +optional
	TenantSelector metav1.LabelSelector `json:"tenantSelector,omitzero"`
}

// GlobalTenantResourceStatus defines the observed state of GlobalTenantResource.
type GlobalTenantResourceStatus struct {
	// List of Tenants addressed by the GlobalTenantResource.
	SelectedTenants []string `json:"selectedTenants"`
	// List of the replicated resources for the given TenantResource.
<<<<<<< HEAD
	ProcessedItems ProcessedItems `json:"processedItems"`
	// Condition of the GlobalTenantResource.
	Conditions meta.ConditionList `json:"conditions,omitempty"`
=======
	ProcessedItems ProcessedItems `json:"processedItems,omitzero"`
}

type ProcessedItems []ObjectReferenceStatus

func (p *ProcessedItems) AsSet() sets.Set[string] {
	set := sets.New[string]()

	for _, i := range *p {
		set.Insert(i.String())
	}

	return set
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status for the tenant"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message for the tenant"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// GlobalTenantResource allows to propagate resource replications to a specific subset of Tenant resources.
type GlobalTenantResource struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec GlobalTenantResourceSpec `json:"spec"`

	// +optional
	Status GlobalTenantResourceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GlobalTenantResourceList contains a list of GlobalTenantResource.
type GlobalTenantResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []GlobalTenantResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlobalTenantResource{}, &GlobalTenantResourceList{})
}
