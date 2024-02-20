// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:object:generate=true

type PodOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Pod resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Allow Specifying Nodeselectors for the pod
	NodeSelectors []PodNodeSelector `json:"nodeSelectors,omitempty"`
}

type PodNodeSelector struct {
	metav1.LabelSelector `json:",inline"`
}
