// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/api"
)

type GatewayOptions struct {
	AllowedClasses *api.SelectionListWithDefaultSpec `json:"allowedClasses,omitempty"`
}
