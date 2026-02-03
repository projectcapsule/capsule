// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

// ResourcePoolSpec.
type ResourcePoolSpec struct {
	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	Selectors []selectors.NamespaceSelector `json:"selectors,omitempty"`
	// Define the resourcequota served by this resourcepool.
	Quota corev1.ResourceQuotaSpec `json:"quota"`
	// The Defaults given for each namespace, the default is not counted towards the total allocation
	// When you use claims it's recommended to provision Defaults as the prevent the scheduling of any resources
	// +optional
	Defaults corev1.ResourceList `json:"defaults,omitzero"`
	// Additional Configuration
	//+kubebuilder:default:={}
	// +optional
	Config ResourcePoolSpecConfiguration `json:"config,omitzero"`
}

type ResourcePoolSpecConfiguration struct {
	// With this option all resources which can be allocated are set to 0 for the resourcequota defaults. (Default false)
	// +kubebuilder:default=false
	DefaultsAssignZero *bool `json:"defaultsZero,omitempty"`
	// Claims are queued whenever they are allocated to a pool. A pool tries to allocate claims in order based on their
	// creation date. But no matter their creation time, if a claim is requesting too much resources it's put into the queue
	// but if a lower priority claim still has enough space in the available resources, it will be able to claim them. Eventough
	// it's priority was lower
	// Enabling this option respects to Order. Meaning the Creationtimestamp matters and if a resource is put into the queue, no
	// other claim can claim the same resources with lower priority. (Default false)
	// +kubebuilder:default=false
	OrderedQueue *bool `json:"orderedQueue,omitempty"`
	// When a resourcepool is deleted, the resourceclaims bound to it are disassociated from the resourcepool but not deleted.
	// By Enabling this option, the resourceclaims will be deleted when the resourcepool is deleted, if they are in bound state. (Default false)
	// +kubebuilder:default=false
	DeleteBoundResources *bool `json:"deleteBoundResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=quotapool
// +kubebuilder:printcolumn:name="Claims",type="integer",JSONPath=".status.claimCount",description="The total amount of Claims bound"
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.namespaceCount",description="The total amount of Namespaces considered"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// Resourcepools allows you to define a set of resources as known from ResoureQuotas. The Resourcepools are defined at cluster-scope an should
// be administrated by cluster-administrators. However they create an interface, where cluster-administrators can define
// from which namespaces resources from a Resourcepool can be claimed. The claiming is done via a namespaced CRD called ResourcePoolClaim. Then
// it's up the group of users within these namespaces, to manage the resources they consume per namespace. Each Resourcepool provisions a ResourceQuotainto all the selected namespaces. Then essentially the ResourcePoolClaims, when they can be assigned to the ResourcePool stack resources on top of that
// ResourceQuota based on the namspace, where the ResourcePoolClaim was made from.
type ResourcePool struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec ResourcePoolSpec `json:"spec"`

	// +optional
	Status ResourcePoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ResourcePoolList contains a list of ResourcePool.
type ResourcePoolList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []ResourcePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePool{}, &ResourcePoolList{})
}
