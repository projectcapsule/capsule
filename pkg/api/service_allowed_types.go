// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true

type AllowedServices struct {
	// +kubebuilder:default=true
	// Specifies if NodePort service type resources are allowed for the Tenant. Default is true. Optional.
	NodePort *bool `json:"nodePort,omitempty"`
	// +kubebuilder:default=true
	// Specifies if ExternalName service type resources are allowed for the Tenant. Default is true. Optional.
	ExternalName *bool `json:"externalName,omitempty"`
	// +kubebuilder:default=true
	// Specifies if LoadBalancer service type resources are allowed for the Tenant. Default is true. Optional.
	LoadBalancer *bool `json:"loadBalancer,omitempty"`
}
