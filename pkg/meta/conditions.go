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
	NotReadyCondition string = "NotReady"

	AssignedCondition string = "Assigned"
	BoundCondition    string = "Bound"

	// FailedReason indicates a condition or event observed a failure (Claim Rejected).
	SucceededReason          string = "Succeeded"
	FailedReason             string = "Failed"
	PoolExhaustedReason      string = "PoolExhausted"
	QueueExhaustedReason     string = "QueueExhausted"
	NamespaceExhaustedReason string = "NamespaceExhausted"
)

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
