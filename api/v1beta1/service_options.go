// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

type ServiceOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Service resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Block or deny certain type of Services. Optional.
	AllowedServices *AllowedServices `json:"allowedServices,omitempty"`
	// Specifies the external IPs that can be used in Services with type ClusterIP. An empty list means no IPs are allowed. Optional.
	ExternalServiceIPs *ExternalServiceIPsSpec `json:"externalIPs,omitempty"`
}
