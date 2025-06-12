// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/api"
)

// GlobalResourceQuotaStatus defines the observed state of GlobalResourceQuota.
type ResourcePoolStatus struct {
	// How many namespaces are considered
	// +kubebuilder:default=0
	NamespaceSize uint `json:"namespaceCount,omitempty"`
	// Amount of claims
	// +kubebuilder:default=0
	ClaimSize uint `json:"claimCount,omitempty"`
	// Namespaces which are considered for claims
	Namespaces []string `json:"namespaces,omitempty"`
	// Tracks the quotas for the Resource.
	Claims ResourcePoolNamespaceClaimsStatus `json:"claims,omitempty"`
	// Tracks the Usage from Claimed against what has been granted from the pool
	Allocation ResourcePoolQuotaStatus `json:"allocation,omitempty"`
	// Exhaustions from claims associated with the pool
	Exhaustions map[string]api.PoolExhaustionResource `json:"exhaustions,omitempty"`
}

type ResourcePoolNamespaceClaimsStatus map[string]ResourcePoolClaimsList

type ResourcePoolQuotaStatus struct {
	// Hard is the set of enforced hard limits for each named resource.
	// More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/
	// +optional
	Hard corev1.ResourceList `json:"hard,omitempty" protobuf:"bytes,1,rep,name=hard,casttype=ResourceList,castkey=ResourceName"`
	// Used is the current observed total usage of the resource in the namespace.
	// +optional
	Claimed corev1.ResourceList `json:"used,omitempty" protobuf:"bytes,2,rep,name=used,casttype=ResourceList,castkey=ResourceName"`
	// Used to track the usage of the resource in the pool (diff hard - claimed). May be used for further automation
	// +optional
	Available corev1.ResourceList `json:"available,omitempty" protobuf:"bytes,2,rep,name=available,casttype=ResourceList,castkey=ResourceName"`
}

type ResourcePoolClaimsList []*ResourcePoolClaimsItem

func (r *ResourcePoolClaimsList) GetClaimByUID(uid types.UID) *ResourcePoolClaimsItem {
	for _, claim := range *r {
		if claim.UID == uid {
			return claim
		}
	}

	return nil
}

// ResourceQuotaClaimStatus defines the observed state of ResourceQuotaClaim.
type ResourcePoolClaimsItem struct {
	// Reference to the GlobalQuota being claimed from
	api.StatusNameUID `json:",inline"`
	// Claimed resources
	Claims corev1.ResourceList `json:"claims,omitempty"`
}
