// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
)

type NamespaceOptions struct {
	// +kubebuilder:validation:Minimum=1
	// Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.
	Quota *int32 `json:"quota,omitempty"`
	// Deprecated: Use additionalMetadataList instead (https://projectcapsule.dev/docs/tenants/metadata/#additionalmetadatalist)
	//
	// Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.
	AdditionalMetadata *api.AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant via a list. Optional.
	AdditionalMetadataList []api.AdditionalMetadataSelectorSpec `json:"additionalMetadataList,omitempty"`
	// Required Metadata for namespace within this tenant
	// +optional
	RequiredMetadata *RequiredMetadata `json:"requiredMetadata,omitzero"`
	// Define the labels that a Tenant Owner cannot set for their Namespace resources.
	// +optional
	ForbiddenLabels api.ForbiddenListSpec `json:"forbiddenLabels,omitzero"`
	// Define the annotations that a Tenant Owner cannot set for their Namespace resources.
	// +optional
	ForbiddenAnnotations api.ForbiddenListSpec `json:"forbiddenAnnotations,omitzero"`
	// If enabled only metadata from additionalMetadata is reconciled to the namespaces.
	//+kubebuilder:default:=false
	ManagedMetadataOnly bool `json:"managedMetadataOnly,omitempty"`
}

type RequiredMetadata struct {
	// Labels that must be defined for each namespace
	// +optional
	Labels map[string]string `json:"labels,omitzero"`

	// Annotations that must be defined for each namespace
	// +optional
	Annotations map[string]string `json:"annotations,omitzero"`
}
