// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// RuleStatus contains the accumulated rules applying to namespace it's deployed in.
// +kubebuilder:object:generate=true
type RuleStatusSpec struct {
	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Managed Enforcement properties per Namespace (aggregated from rules)
	//+optional
	Rule api.NamespaceRuleBodyNamespace `json:"rule,omitzero"`
	// Conditions
	Conditions meta.ConditionList `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"
type RuleStatus struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec []*api.NamespaceRuleBodyNamespace `json:"spec,omitzero"`

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
