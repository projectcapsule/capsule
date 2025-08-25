// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

type ResourcePoolClaimSpec struct {
	// If there's the possability to claim from multiple global Quotas
	// You must be specific about which one you want to claim resources from
	// Once bound to a ResourcePool, this field is immutable
	Pool string `json:"pool"`
	// Amount which should be claimed for the resourcequota
	ResourceClaims corev1.ResourceList `json:"claim"`
}

// ResourceQuotaClaimStatus defines the observed state of ResourceQuotaClaim.
type ResourcePoolClaimStatus struct {
	// Reference to the GlobalQuota being claimed from
	Pool api.StatusNameUID `json:"pool,omitempty"`
	// Condtion for this resource claim
	Condition metav1.Condition `json:"condition,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool",type="string",JSONPath=".status.pool.name",description="The ResourcePool being claimed from"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.condition.type",description="Status for claim"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.condition.reason",description="Reason for status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.condition.message",description="Condition Message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""

// ResourcePoolClaim is the Schema for the resourcepoolclaims API.
type ResourcePoolClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePoolClaimSpec   `json:"spec,omitempty"`
	Status ResourcePoolClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceQuotaClaimList contains a list of ResourceQuotaClaim.
type ResourcePoolClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ResourcePoolClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePoolClaim{}, &ResourcePoolClaimList{})
}
