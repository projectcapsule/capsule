// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
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
	Sources []CustomQuotaSpecSource `json:"sources,omitzero"`
	// Additional Options for the CustomQuotaSpecification
	// +kubebuilder:default:={emitMetricPerClaimUsage:false}
	Options *CustomQuotaOptionsSpec `json:"options,omitzero"`
}

// CustomQuotaOptionsSpec.
type CustomQuotaOptionsSpec struct {
	// Additionaly expose usage metrics for each claim contributing to the quota.
	// This is disabled by default to avoid high cardinality in the metrics, but can be enabled for more granular monitoring and alerting.
	// +kubebuilder:default:=false
	EmitPerClaimMetrics bool `json:"emitMetricPerClaimUsage,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.op == 'count' ? !has(self.path) || size(self.path) == 0 : has(self.path) && size(self.path) > 0",message="path must be empty when op is 'count'; otherwise path must be set and non-empty"
type CustomQuotaSpecSource struct {
	metav1.GroupVersionKind `json:",inline"`

	// Path on GVK where usage is evaluated.
	// Must be empty when op is "count".
	// Required and non-empty for all other operations.
	// +optional
	Path string `json:"path,omitempty"`

	// Operation used to evaluate usage.
	// +kubebuilder:default:=add
	Operation quota.Operation `json:"op,omitempty"`

	// Provide more granular selectors for these sources
	// The ScopeSelector and NamespaceSelector are always applied
	// Allowing these selectors to make further selecting on the resulting subset.
	Selectors []selectors.SelectorWithFields `json:"selectors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Limit",type="string",JSONPath=".spec.limit",description="The total limit available"
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
