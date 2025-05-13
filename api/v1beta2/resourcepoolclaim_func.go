// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"github.com/projectcapsule/capsule/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Ensures the condition is only updated if the status is different
// Otherwise we cause infinite updates because of the timestamp
func (r *ResourcePoolClaim) UpdateCondition(condition metav1.Condition) {
	if r.Status.Condition.Reason == condition.Reason || r.Status.Condition.Message == condition.Message {
		return
	}

	r.Status.Condition = condition
}

// Indicate the claim is bound to a resource pool
func (r *ResourcePoolClaim) IsBoundToResourcePool() bool {
	if r.Status.Condition.Reason == meta.BoundReason &&
		r.Status.Condition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}
