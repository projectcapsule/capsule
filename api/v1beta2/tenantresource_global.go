// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// +kubebuilder:validation:Enum=Namespace;Tenant
type GlobalTenantResourceScope string

func (p GlobalTenantResourceScope) String() string {
	return string(p)
}

const (
	GlobalTenantResourceScopeNamespace GlobalTenantResourceScope = "Namespace"
	GlobalTenantResourceScopeTenant    GlobalTenantResourceScope = "Tenant"
)

// GlobalTenantResourceSpec defines the desired state of GlobalTenantResource.
type GlobalTenantResourceSpec struct {
	// Resource Scope, Can either be
	// - Tenant: Create Resources for each tenant  in selected Tenants
	// - Namespace: Create Resources for each namespace in selected Tenants
	// +kubebuilder:default:=Namespace
	Scope GlobalTenantResourceScope `json:"scope"`
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
	// Condition of the GlobalTenantResource.
	Condition api.Condition `json:"condition,omitempty"`
}

func (p *GlobalTenantResource) SetCondition() {
	failures := 0

	for _, item := range p.Status.ProcessedItems {
		if item.Status != metav1.ConditionTrue {
			failures++
		}
	}

	cond := meta.NewReadyCondition(p)
	if failures > 0 {
		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.FailedReason
		cond.Message = "Reconcile completed with errors"
	} else {
		cond.Status = metav1.ConditionTrue
		cond.Reason = meta.SucceededReason
		cond.Message = "Reconcile completed successfully"
	}

	p.Status.Condition.UpdateCondition(cond)
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
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.condition.type",description="Status for claim"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.condition.reason",description="Reason for status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""

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
