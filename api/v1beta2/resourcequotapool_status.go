// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GlobalResourceQuotaStatus defines the observed state of GlobalResourceQuota
type ResourceQuotaPoolStatus struct {
	// List of namespaces assigned to the Tenant.
	Namespaces []string `json:"namespaces,omitempty"`
	// Tracks the quotas for the Resource.
	Claims ResourceQuotaPoolClaimsList `json:"claims,omitempty"`
	// Queue to track claims, which could not be allocated but are waiting for resources
	// Mainly used to keep track of the priority based on when the claims were created.
	Queue ResourceQuotaPoolClaimsList `json:"queue,omitempty"`
	// Tracks the Usage from Claimed against what has been granted from the pool
	Allocation corev1.ResourceQuotaStatus `json:"allocation,omitempty"`
}

type ResourceQuotaPoolClaimsList []*ResourceQuotaPoolClaimsItem

func (r *ResourceQuotaPoolClaimsList) GetClaimByUID(uid types.UID) *ResourceQuotaPoolClaimsItem {
	for _, claim := range *r {
		if claim.UID == uid {
			return claim
		}
	}
	return nil
}

// ResourceQuotaClaimStatus defines the observed state of ResourceQuotaClaim.
type ResourceQuotaPoolClaimsItem struct {
	// Reference to the GlobalQuota being claimed from
	api.StatusNameUID `json:",inline"`
	// Claimed resources
	Claims corev1.ResourceList `json:"claims,omitempty"`
}
