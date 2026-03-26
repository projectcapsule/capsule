// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

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
	Target CustomQuotaStatusTarget `json:"target"`
	// Conditions
	Conditions meta.ConditionList `json:"conditions"`
}

func (s *CustomQuotaStatus) HasClaimUID(uid types.UID) bool {
	for i := range s.Claims {
		if s.Claims[i].UID == uid {
			return true
		}
	}

	return false
}

type CustomQuotaStatusTarget struct {
	CustomQuotaSpecSource `json:",inline"`

	// Path on GVK where usage is evaluated
	Scope k8smeta.RESTScopeName `json:"scope,omitempty"`
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
