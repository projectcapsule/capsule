// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ReadyCondition indicates the resource is ready and fully reconciled.
	ReadyCondition    string = "Ready"
	CordonedCondition string = "Cordoned"
	NotReadyCondition string = "NotReady"

	AssignedCondition string = "Assigned"
	BoundCondition    string = "Bound"

	// FailedReason indicates a condition or event observed a failure (Claim Rejected).
	SucceededReason          string = "Succeeded"
	FailedReason             string = "Failed"
	ActiveReason             string = "Active"
	CordonedReason           string = "Cordoned"
	PoolExhaustedReason      string = "PoolExhausted"
	QueueExhaustedReason     string = "QueueExhausted"
	NamespaceExhaustedReason string = "NamespaceExhausted"
)

// +kubebuilder:object:generate=true

type ConditionList []Condition

// Adds a condition by type.
func (c *ConditionList) GetConditionByType(conditionType string) *Condition {
	for i := range *c {
		if (*c)[i].Type == conditionType {
			return &(*c)[i]
		}
	}

	return nil
}

// Adds a condition by type.
func (c *ConditionList) UpdateConditionByType(condition Condition) {
	for i, cond := range *c {
		if cond.Type == condition.Type {
			(*c)[i].UpdateCondition(condition)

			return
		}
	}

	*c = append(*c, condition)
}

// Removes a condition by type.
func (c *ConditionList) RemoveConditionByType(condition Condition) {
	if c == nil {
		return
	}

	filtered := make(ConditionList, 0, len(*c))

	for _, cond := range *c {
		if cond.Type != condition.Type {
			filtered = append(filtered, cond)
		}
	}

	*c = filtered
}

// +kubebuilder:object:generate=true
type Condition metav1.Condition

func NewReadyCondition(obj client.Object) Condition {
	return Condition{
		Type:               ReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             SucceededReason,
		Message:            "reconciled",
		LastTransitionTime: metav1.Now(),
	}
}

func NewCordonedCondition(obj client.Object) Condition {
	return Condition{
		Type:               CordonedCondition,
		Status:             metav1.ConditionFalse,
		Reason:             ActiveReason,
		Message:            "not cordoned",
		LastTransitionTime: metav1.Now(),
	}
}

// Disregards fields like LastTransitionTime and Version, which are not relevant for the API.
func (c *Condition) UpdateCondition(condition Condition) (updated bool) {
	if condition.Type == c.Type &&
		condition.Status == c.Status &&
		condition.Reason == c.Reason &&
		condition.Message == c.Message &&
		condition.ObservedGeneration == c.ObservedGeneration {
		return false
	}

	if condition.Status != c.Status {
		c.LastTransitionTime = metav1.Now()
	}

	c.Type = condition.Type
	c.Status = condition.Status
	c.Reason = condition.Reason
	c.Message = condition.Message
	c.ObservedGeneration = condition.ObservedGeneration
	c.LastTransitionTime = condition.LastTransitionTime

	return true
}

func NewBoundCondition(obj client.Object) metav1.Condition {
	return metav1.Condition{
		Type:               BoundCondition,
		ObservedGeneration: obj.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
}

func NewAssignedCondition(obj client.Object) metav1.Condition {
	return metav1.Condition{
		Type:               AssignedCondition,
		ObservedGeneration: obj.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
}
