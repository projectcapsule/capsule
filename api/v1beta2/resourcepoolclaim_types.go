// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
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
	// +optional
	Pool meta.LocalRFC1123ObjectReferenceWithUID `json:"pool,omitzero"`
	// Deprecated: Use Conditions
	//
	// +optional
	Condition metav1.Condition `json:"condition,omitzero"`
	// Conditions for the resource claim
	Conditions meta.ConditionList `json:"conditions,omitzero"`
	// Tracks the Usage from Claimed from this claim and available resources
	// +optional
	Allocation ResourcePoolQuotaStatus `json:"allocation,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool",type="string",JSONPath=".status.pool.name",description="The ResourcePool being claimed from"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Ready Status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Ready Message"
// +kubebuilder:printcolumn:name="Bound",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].status",description="Bound Status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].message",description="Bound Message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
// ResourcePoolClaim is the Schema for the resourcepoolclaims API.
type ResourcePoolClaim struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec ResourcePoolClaimSpec `json:"spec"`

	// +optional
	Status ResourcePoolClaimStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true
// ResourceQuotaClaimList contains a list of ResourceQuotaClaim.
type ResourcePoolClaimList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []ResourcePoolClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePoolClaim{}, &ResourcePoolClaimList{})
}
