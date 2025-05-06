// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourcePoolUsageStatus defines the observed state of ResourcePoolUsage.
// It's main purpose is to allow for endusers to see, what they still can allocate from
// a namespace. In addition it's used to keep track of all resources allocated for total calculations
type ResourcePoolUsageStatus struct {
	// Tracks the quotas for the Resource.
	Claims ResourcePoolClaimsList `json:"claims,omitempty"`
	// Tracks the Usage from Claimed against what has been granted from the pool
	Allocation ResourcePoolQuotaStatus `json:"allocation,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ResourcePoolUsage is the Schema for the resourcepoolusages API.
type ResourcePoolUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ResourcePoolUsageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePoolUsageList contains a list of ResourcePoolUsage.
type ResourcePoolUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePoolUsage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePoolUsage{}, &ResourcePoolUsageList{})
}
