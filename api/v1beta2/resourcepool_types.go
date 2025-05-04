// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// GlobalResourceQuotaSpec defines the desired state of GlobalResourceQuota
type ResourcePoolSpec struct {
	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	Selectors []api.NamespaceSelector `json:"selectors,omitempty"`
	// Define resourcequotas for the namespaces
	Quota corev1.ResourceQuotaSpec `json:"quota"`
	// The maxmum amount of resources that can be claimed from a resourcequota in a namespace
	MaximumNamespaceAllocation corev1.ResourceList `json:"namespaceMaximum,omitempty"`
	// The Defaults given for each namespace, the default is not counted towards the total allocation
	// When you use claims it's recommended to provision Defaults as the prevent the scheduling of any resources
	Defaults corev1.ResourceList `json:"defaults,omitempty"`
	// Additional Configuration
	//+kubebuilder:default:={}
	Config ResourcePoolSpecConfiguration `json:"config,omitempty"`
}

type ResourcePoolSpecConfiguration struct {
	// Enable Distribution of Defaults for each namespace
	// This allocates the default resources to each resourcequota responsible for a namespace.
	// The Defaults serve as a base for the resource allocation, and are not counted towards the total allocation
	// +kubebuilder:default=true
	DefaultsAssignZero *bool `json:"defaultsZero,omitempty"`

	// Claims are queued whenever they are allocated to a pool. A pool tries to allocate claims in order based on their
	// creation date.
	// Disabling this option will cause the resource pool to allocate claims which still fit in the remaining available resources.
	// This disregards any ordering of the claims.
	// +kubebuilder:default=false
	OrderedQueue *bool `json:"orderedQueue,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=quotapool
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.size",description="The total amount of Namespaces spanned across"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

type ResourcePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePoolSpec   `json:"spec,omitempty"`
	Status ResourcePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GlobalResourceQuotaList contains a list of GlobalResourceQuota
type ResourcePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePool{}, &ResourcePoolList{})
}
