// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TenantPermissionSpec defines the desired state of TenantPermission
type TenantPermissionSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of TenantPermission. Edit tenantpermission_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// TenantPermissionStatus defines the observed state of TenantPermission
type TenantPermissionStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TenantPermission is the Schema for the tenantpermissions API
type TenantPermission struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantPermissionSpec   `json:"spec,omitempty"`
	Status TenantPermissionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantPermissionList contains a list of TenantPermission
type TenantPermissionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TenantPermission `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TenantPermission{}, &TenantPermissionList{})
}
