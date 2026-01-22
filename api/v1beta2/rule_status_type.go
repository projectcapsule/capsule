// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"
type RuleStatus struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Status RuleStatusSpec `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RuleStatusList contains a list of RuleStatus.
type RuleStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []RuleStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RuleStatus{}, &RuleStatusList{})
}

// RuleStatus contains the accumulated rules applying to namespace it's deployed in.
// +kubebuilder:object:generate=true
type RuleStatusSpec struct {
	// Managed Enforcement properties per Namespace (aggregated from rules)
	//+optional
	Rule NamespaceRuleBody `json:"rule,omitzero"`
}
