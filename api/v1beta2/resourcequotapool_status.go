// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

// GlobalResourceQuotaStatus defines the observed state of GlobalResourceQuota
type ResourceQuotaPoolStatus struct {
	// List of namespaces assigned to the Tenant.
	Namespaces []string `json:"namespaces,omitempty"`
	// Tracks the quotas for the Resource.
	Claims []*ResourceQuotaPoolClaimsItem `json:"claims,omitempty"`
	// Tracks the Usage from Claimed against what has been granted from the pool
	Allocation corev1.ResourceQuotaStatus
}

// ResourceQuotaClaimStatus defines the observed state of ResourceQuotaClaim.
type ResourceQuotaPoolClaimsItem struct {
	// Reference to the GlobalQuota being claimed from
	api.StatusNameUID `json:",inline"`
	// Claimed resources
	Claims corev1.ResourceList `json:"claims,omitempty"`
}
