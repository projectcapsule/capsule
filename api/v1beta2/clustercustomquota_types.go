// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterCustomQuotaSpec.
type ClusterCustomQuotaSpec struct {
	CustomQuotaSpec `json:",inline"`

	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Used",type="string",JSONPath=".status.used",description="The total used amount"
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".spec.limit",description="The total limit available"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.available",description="The total amount available"

type ClusterCustomQuota struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec ClusterCustomQuotaSpec `json:"spec"`

	// +optional
	Status CustomQuotaStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterCustomQuotaList contains a list of ClusterCustomQuota.
type ClusterCustomQuotaList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []ClusterCustomQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterCustomQuota{}, &ClusterCustomQuotaList{})
}
