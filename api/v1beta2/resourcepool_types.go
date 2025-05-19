// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// ResourcePoolSpec.
type ResourcePoolSpec struct {
	// Selector to match the namespaces that should be managed by the GlobalResourceQuota
	Selectors []api.NamespaceSelector `json:"selectors,omitempty"`
	// Define the resourcequota served by this resourcepool.
	Quota corev1.ResourceQuotaSpec `json:"quota"`
	// The Defaults given for each namespace, the default is not counted towards the total allocation
	// When you use claims it's recommended to provision Defaults as the prevent the scheduling of any resources
	Defaults corev1.ResourceList `json:"defaults,omitempty"`
	// Additional Configuration
	//+kubebuilder:default:={}
	Config ResourcePoolSpecConfiguration `json:"config,omitempty"`
}

type ResourcePoolSpecConfiguration struct {
	// With this option all resources which can be allocated are set to 0 for the resourcequota defaults.
	// +kubebuilder:default=false
	DefaultsAssignZero *bool `json:"defaultsZero,omitempty"`
	// Claims are queued whenever they are allocated to a pool. A pool tries to allocate claims in order based on their
	// creation date. But no matter their creation time, if a claim is requesting too much resources it's put into the queue
	// but if a lower priority claim still has enough space in the available resources, it will be able to claim them. Eventough
	// it's priority was lower
	// Enabling this option respects to Order. Meaning the Creationtimestamp matters and if a resource is put into the queue, no
	// other claim can claim the same resources with lower priority.
	// +kubebuilder:default=false
	OrderedQueue *bool `json:"orderedQueue,omitempty"`
	// When a resourcepool is deleted, the resourceclaims bound to it are disassociated from the resourcepool but not deleted.
	// By Enabling this option, the resourceclaims will be deleted when the resourcepool is deleted, if they are in bound state.
	// +kubebuilder:default=false
	DeleteBoundResources *bool `json:"deleteBoundResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=quotapool
// +kubebuilder:printcolumn:name="Claims",type="integer",JSONPath=".status.claimCount",description="The total amount of Claims bound"
// +kubebuilder:printcolumn:name="Namespaces",type="integer",JSONPath=".status.namespaceCount",description="The total amount of Namespaces considered"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// Resourcepools allows you to define a set of resources as known from ResoureQuotas. The Resourcepools are defined at cluster-scope an should
// be administrated by cluster-administrators. However they create an interface, where cluster-administrators can define
// from which namespaces resources from a Resourcepool can be claimed. The claiming is done via a namespaced CRD called ResourcePoolClaim. Then
// it's up the group of users within these namespaces, to manage the resources they consume per namespace. Each Resourcepool provisions a ResourceQuotainto all the selected namespaces. Then essentially the ResourcePoolClaims, when they can be assigned to the ResourcePool stack resources on top of that
// ResourceQuota based on the namspace, where the ResourcePoolClaim was made from.
type ResourcePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePoolSpec   `json:"spec,omitempty"`
	Status ResourcePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePoolList contains a list of ResourcePool.
type ResourcePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourcePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourcePool{}, &ResourcePoolList{})
}
