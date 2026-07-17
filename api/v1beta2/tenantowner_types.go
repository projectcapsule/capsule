// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// TenantOwnerSpec defines the desired state of TenantOwner.
type TenantOwnerSpec struct {
	// Subject
	rbac.CoreOwnerSpec `json:",inline"`

	// Adds the given subject as capsule user. When enabled this subject does not have to be
	// mentioned in the CapsuleConfiguration as Capsule User. In almost all scenarios Tenant Owners
	// must be Capsule Users.
	// +kubebuilder:default=true
	// +optional
	Aggregate *bool `json:"aggregate,omitempty"`
}

func (s TenantOwnerSpec) AggregateEnabled() bool {
	return s.Aggregate == nil || *s.Aggregate
}

// TenantOwnerStatus defines the observed state of TenantOwner.
type TenantOwnerStatus struct {
	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Tenants lists the names of all Tenants that this TenantOwner is currently matched to
	// via the Tenant's spec.permissions.matchOwners selectors.
	// +optional
	// +listType=atomic
	Tenants []string `json:"tenants,omitempty"`

	// Conditions contains the reconciliation conditions for this TenantOwner.
	// +optional
	Conditions meta.ConditionList `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=to
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile status of this TenantOwner"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// TenantOwner is the Schema for the tenantowners API.
type TenantOwner struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TenantOwner.
	// +required
	Spec TenantOwnerSpec `json:"spec"`

	// status defines the observed state of TenantOwner.
	// +optional
	Status TenantOwnerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TenantOwnerList contains a list of TenantOwner.
type TenantOwnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []TenantOwner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TenantOwner{}, &TenantOwnerList{})
}
