// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Indicate the claim is bound to a resource pool.
func (r *ResourcePoolClaim) IsExhaustedInResourcePool() bool {
	condition := r.Status.Conditions.GetConditionByType(meta.ExhaustedCondition)

	if condition == nil {
		return false
	}

	if condition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func (r *ResourcePoolClaim) IsAssignedInResourcePool() bool {
	condition := r.Status.Conditions.GetConditionByType(meta.AssignedCondition)

	if condition == nil {
		return false
	}

	if condition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func (r *ResourcePoolClaim) IsBoundInResourcePool() bool {
	condition := r.Status.Conditions.GetConditionByType(meta.BoundCondition)

	if condition == nil {
		return false
	}

	if condition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func (r *ResourcePoolClaim) GetPool() string {
	if name := string(r.Status.Pool.Name); name != "" {
		return name
	}

	return r.Spec.Pool
}
