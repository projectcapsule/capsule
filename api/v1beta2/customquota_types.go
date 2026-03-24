// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CustomQuotaSpec.
type CustomQuotaSpec struct {
	// Select items governed by this quota
	ScopeSelectors []metav1.LabelSelector `json:"scopeSelectors,omitempty"`
	// Resource Quantity as limit
	Limit resource.Quantity `json:"limit"`
	// Target resource
	Source CustomQuotaSpecSource `json:"source,omitzero"`
}

type CustomQuotaSpecSource struct {
	metav1.GroupVersionKind `json:",inline"`

	// Path on GVK where usage is evaluated
	Path string `json:"path,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".status.spec.limit",description="The total limit available"
// +kubebuilder:printcolumn:name="Used",type="string",JSONPath=".status.usage.used",description="The total used amount"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.usage.available",description="The total amount available"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message"

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
