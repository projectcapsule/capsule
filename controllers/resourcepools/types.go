// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

type PoolExhaustion map[string]PoolExhaustionResource

type PoolExhaustionResource struct {
	Namespace  bool
	Available  resource.Quantity
	Requesting resource.Quantity
}
