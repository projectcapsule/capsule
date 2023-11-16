// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true

type ServiceOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Block or deny certain type of Services. Optional.
	AllowedServices *AllowedServices `json:"allowedServices,omitempty"`
	// Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.
	ExternalServiceIPs *ExternalServiceIPsSpec `json:"externalIPs,omitempty"`
	// Define the labels that a Tenant Owner cannot set for their Service resources.
	ForbiddenLabels ForbiddenListSpec `json:"forbiddenLabels,omitempty"`
	// Define the annotations that a Tenant Owner cannot set for their Service resources.
	ForbiddenAnnotations ForbiddenListSpec `json:"forbiddenAnnotations,omitempty"`
}
