// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

// ClusterCustomQuotaSpec.
type GlobalCustomQuotaSpec struct {
	CustomQuotaSpec `json:",inline"`

	// Select specifc namespaces where this Quota selects items.
	NamespaceSelectors []selectors.NamespaceSelector `json:"namespaceSelectors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".spec.limit",description="The total limit available"
// +kubebuilder:printcolumn:name="Used",type="string",JSONPath=".status.usage.used",description="The total used amount"
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.usage.available",description="The total amount available"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message"

type GlobalCustomQuota struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec GlobalCustomQuotaSpec `json:"spec"`

	// +optional
	Status GlobalCustomQuotaStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterCustomQuotaList contains a list of ClusterCustomQuota.
type GlobalCustomQuotaList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []GlobalCustomQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlobalCustomQuota{}, &GlobalCustomQuotaList{})
}
