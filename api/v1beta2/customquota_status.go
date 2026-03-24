// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// CustomQuotaStatus defines the observed state of GlobalResourceQuota.
type CustomQuotaStatus struct {
	// Usage measurements
	// +optional
	Usage CustomQuotaStatusUsage `json:"usage,omitempty"`
	// Objects regarding this policy
	Claims []meta.NamespacedObjectWithUIDReference `json:"claims,omitempty"`
	// Targeting GVK
	Target CustomQuotaSpecSource `json:"target"`
	// Conditions
	Conditions meta.ConditionList `json:"conditions"`
}

// CustomQuotaStatus defines the observed state of GlobalResourceQuota.
type CustomQuotaStatusUsage struct {
	// Used is the current observed total usage of the resource.
	// +optional
	Used resource.Quantity `json:"used"`
	// Used is the current observed total available of the resource (limit - used).
	// +optional
	Available resource.Quantity `json:"available"`
}
