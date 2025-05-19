// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func PoolResourceQuotaName(quota *capsulev1beta2.ResourcePool) string {
	return fmt.Sprintf("capsule-pool-%s", quota.Name)
}
