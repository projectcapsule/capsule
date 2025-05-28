// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:generate=true
type Condition metav1.Condition

// Disregards fields like LastTransitionTime and Version, which are not relevant for the API.
func (c *Condition) UpdateCondition(condition metav1.Condition) (updated bool) {
	if condition.Type == c.Type &&
		condition.Status == c.Status &&
		condition.Reason == c.Reason &&
		condition.Message == c.Message {
		return false
	}

	c.Type = condition.Type
	c.Status = condition.Status
	c.Reason = condition.Reason
	c.Message = condition.Message
	c.ObservedGeneration = condition.ObservedGeneration
	c.LastTransitionTime = condition.LastTransitionTime

	return true
}
