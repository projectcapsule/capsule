// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// RuleStatus contains the accumulated rules applying to namespace it's deployed in.
// +kubebuilder:object:generate=true
type RuleStatusStatus struct {
	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Deprecated: use Rules.
	// Rule contains a legacy flattened view and cannot fully represent action-aware rules.
	// +optional
	Rule rules.NamespaceRuleBodyNamespace `json:"rule,omitzero"`
	// Rules contains the effective namespace rules after tenant rule selection.
	// Order is preserved from the originating Tenant rules.
	// +optional
	Rules []*rules.NamespaceRuleBodyNamespace `json:"rules,omitempty"`
	// Conditions
	Conditions meta.ConditionList `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Ready Status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Ready Message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"
type RuleStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec []*rules.NamespaceRuleBodyNamespace `json:"spec,omitzero"`

	// +optional
	Status RuleStatusStatus `json:"status,omitzero"`
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
