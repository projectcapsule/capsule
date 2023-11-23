// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true

type PodOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Pod resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
}
