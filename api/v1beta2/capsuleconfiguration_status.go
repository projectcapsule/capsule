// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// CapsuleConfigurationStatus defines the Capsule configuration status.
type CapsuleConfigurationStatus struct {
	// Users which are considered Capsule Users and are bound to the Capsule Tenant construct.
	Users rbac.UserListSpec `json:"users,omitempty"`
	// Conditions holds the reconciliation conditions for this CapsuleConfiguration.
	// Includes a Ready condition indicating whether the configuration was
	// successfully validated and applied.
	// +optional
	Conditions meta.ConditionList `json:"conditions,omitempty"`
	// Tenants is the sorted list of Tenant names currently present in the cluster.
	// The total count is available via len(Tenants).
	// +listType=atomic
	// +optional
	Tenants []string `json:"tenants,omitempty"`
	// ObservedGeneration is the most recent generation the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}
