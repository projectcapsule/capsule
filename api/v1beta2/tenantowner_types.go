// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// TenantOwnerSpec defines the desired state of TenantOwner.
type TenantOwnerSpec struct {
	api.CoreOwnerSpec `json:",inline"`
}

// TenantOwnerStatus defines the observed state of TenantOwner.
type TenantOwnerStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

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
