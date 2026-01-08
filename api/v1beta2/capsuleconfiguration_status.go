// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
)

// CapsuleConfigurationStatus defines the Capsule configuration status.
type CapsuleConfigurationStatus struct {
	// Users which are considered Capsule Users and are bound to the Capsule Tenant construct.
	Users api.UserListSpec `json:"users,omitempty"`
}
