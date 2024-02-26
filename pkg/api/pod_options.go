// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	corev1 "k8s.io/api/core/v1"
)

// +kubebuilder:object:generate=true

type PodOptions struct {
	// Specifies additional labels and annotations the Capsule operator places on any Pod resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
	// Allow Specifying Nodeselectors for the pod
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// Allow Specifying Tolerations for the pod
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Allow Specifying TopologySpreadConstraints for the pod
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}
