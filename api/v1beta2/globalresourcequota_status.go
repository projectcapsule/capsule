// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

// GlobalResourceQuotaStatus defines the observed state of GlobalResourceQuota
type GlobalResourceQuotaStatus struct {
	// If this quota is active or not.
	// +kubebuilder:default=true
	Active bool `json:"active"`
	// How many namespaces are assigned to the Tenant.
	// +kubebuilder:default=0
	Size uint `json:"size"`
	// List of namespaces assigned to the Tenant.
	Namespaces []string `json:"namespaces,omitempty"`
	// Tracks the quotas for the Resource.
	Quota GlobalResourceQuotaStatusQuota `json:"quotas,omitempty"`
}

type GlobalResourceQuotaStatusQuota map[api.Name]*corev1.ResourceQuotaStatus
