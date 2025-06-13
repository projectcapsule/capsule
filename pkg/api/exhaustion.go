// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

// +kubebuilder:object:generate=true
type PoolExhaustionResource struct {
	// Available Resources to be claimed
	Available resource.Quantity `json:"available,omitempty"`
	// Requesting Resources
	Requesting resource.Quantity `json:"requesting,omitempty"`
}
