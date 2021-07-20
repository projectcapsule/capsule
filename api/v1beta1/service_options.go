// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

type ServiceOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Block or deny certain type of Services. Optional.
	AllowedServices *AllowedServices `json:"allowedServices,omitempty"`
}

type AllowedServices struct {
	//+kubebuilder:default=true
	// Specifies if NodePort service type resources are allowed for the Tenant. Default is true. Optional.
	NodePort *bool `json:"nodePort,omitempty"`
	//+kubebuilder:default=true
	// Specifies if ExternalName service type resources are allowed for the Tenant. Default is true. Optional.
	ExternalName *bool `json:"externalName,omitempty"`
}
