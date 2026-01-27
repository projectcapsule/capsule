// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

// CapsuleConfigurationStatus defines the Capsule configuration status.
type CapsuleConfigurationStatus struct {
	// Last time all caches were invalided
	LastCacheInvalidation metav1.Time `json:"lastCacheInvalidation,omitempty"`

	// Users which are considered Capsule Users and are bound to the Capsule Tenant construct.
	Users api.UserListSpec `json:"users,omitempty"`
}
