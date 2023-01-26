// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/clastix/capsule/pkg/api"
)

type NamespaceOptions struct {
	// +kubebuilder:validation:Minimum=1
	// Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.
	Quota *int32 `json:"quota,omitempty"`
	// Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.
	AdditionalMetadata *api.AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Define the labels that a Tenant Owner cannot set for their Namespace resources.
	ForbiddenLabels api.ForbiddenListSpec `json:"forbiddenLabels,omitempty"`
	// Define the annotations that a Tenant Owner cannot set for their Namespace resources.
	ForbiddenAnnotations api.ForbiddenListSpec `json:"forbiddenAnnotations,omitempty"`
}
