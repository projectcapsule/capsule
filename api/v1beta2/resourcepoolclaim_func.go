// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Indicate the claim is bound to a resource pool.
func (r *ResourcePoolClaim) IsBoundToResourcePool() bool {
	if r.Status.Condition.Type == meta.BoundCondition &&
		r.Status.Condition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}
