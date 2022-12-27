// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

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
