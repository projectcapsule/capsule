// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CustomQuotaSpec.
type CustomQuotaSpec struct {
	Selectors []metav1.LabelSelector `json:"selectors,omitempty"`
	Limit     resource.Quantity      `json:"limit,omitempty"`
	//+kubebuilder:default:={}
	Source CustomQuotaSpecSource `json:"source,omitzero"`
}

type CustomQuotaSpecSource struct {
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Path    string `json:"path,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Used",type="string",JSONPath=".status.used",description="The total used amount"
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".spec.limit",description="The total limit available"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.available",description="The total amount available"

type CustomQuota struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec CustomQuotaSpec `json:"spec"`

	// +optional
	Status CustomQuotaStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CustomQuotaList contains a list of CustomQuota.
type CustomQuotaList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []CustomQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CustomQuota{}, &CustomQuotaList{})
}
