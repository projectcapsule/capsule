// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import corev1 "k8s.io/api/core/v1"

// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type ImagePullPolicySpec string

func (i ImagePullPolicySpec) String() string {
	return string(i)
}

// +kubebuilder:validation:Enum=Images;Volumes
type RegistryValidationTarget string

const (
	ValidateImages  RegistryValidationTarget = "Images"
	ValidateVolumes RegistryValidationTarget = "Volumes"
)

// +kubebuilder:object:generate=true
type OCIRegistry struct {
	// OCI Registry endpoint, is treated as regular expression.
	Registry string `json:"url,omitzero"`

	// Allowed PullPolicy for the given registry. Supplying no value allows all policies.
	// +optional
	Policy []corev1.PullPolicy `json:"policy,omitempty"`

	// Requesting Resources
	//+kubebuilder:default:={Images,Volumes}
	Validation []RegistryValidationTarget `json:"validation,omitempty"`
}
