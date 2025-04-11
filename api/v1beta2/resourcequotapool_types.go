// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// GlobalResourceQuotaSpec defines the desired state of GlobalResourceQuota
type ResourceQuotaPoolSpec struct {
	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	Selectors []ResourceQuotaPoolSelector `json:"selectors,omitempty"`

	// Define resourcequotas for the namespaces
	Quota corev1.ResourceQuotaSpec `json:"quota,omitempty"`

	// The maxmum amount of resources that can be claimed from a resourcequota in a namespace
	MaximumAllocation corev1.ResourceList `json:"namespaceMaximum,omitempty"`

	// The Defaults given for each namespace, the default is not counted towards the total allocation
	// When you use claims it's recommended to provision Defaults as the prevent the scheduling of any resources
	Defaults corev1.ResourceList `json:"namespaceDefaults,omitempty"`
}

type ResourceQuotaPoolSelector struct {
	// Only considers namespaces which are part of a tenant, other namespaces which might match
	// the label, but do not have a tenant, are ignored.
	// +kubebuilder:default=true
	MustTenantNamespace bool `json:"tenant,omitempty"`

	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	api.NamespaceSelector `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=globalquota
// +kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active",description="Active status of the GlobalResourceQuota"
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.size",description="The total amount of Namespaces spanned across"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// GlobalResourceQuota is the Schema for the globalresourcequotas API
type ResourceQuotaPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceQuotaPoolSpec   `json:"spec,omitempty"`
	Status ResourceQuotaPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GlobalResourceQuotaList contains a list of GlobalResourceQuota
type ResourceQuotaPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceQuotaPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceQuotaPool{}, &ResourceQuotaPoolList{})
}
