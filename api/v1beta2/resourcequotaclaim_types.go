// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// ResourceQuotaClaimSpec defines the desired state of ResourceQuotaClaim.
type ResourceQuotaClaimSpec struct {
	// If there's the possability to claim from multiple global Quotas
	// You must be specific about which one you want to claim resources from
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	Pool string `json:"pool,omitempty"`
	// Amount which should be claimed for the resourcequota
	ResourceClaims corev1.ResourceList `json:"claim"`
}

// ResourceQuotaClaimStatus defines the observed state of ResourceQuotaClaim.
type ResourceQuotaClaimStatus struct {
	// Reference to the GlobalQuota being claimed from
	GlobalQuota api.StatusNameUID `json:"globalQuota,omitempty"`
	// Condtion for this resource claim
	Condition metav1.Condition `json:"condition,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
// +kubebuilder:printcolumn:name="Quota",type="string",JSONPath=".status.globalQuota.name",description="The Global ResourceQuota being consumed"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.condition.reason",description="Condition"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.condition.message",description="Condition Message"

// ResourceQuotaClaim is the Schema for the resourcequotaclaims API.
type ResourceQuotaClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceQuotaClaimSpec   `json:"spec,omitempty"`
	Status ResourceQuotaClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceQuotaClaimList contains a list of ResourceQuotaClaim.
type ResourceQuotaClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceQuotaClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceQuotaClaim{}, &ResourceQuotaClaimList{})
}
