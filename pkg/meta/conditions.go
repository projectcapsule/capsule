// Copyright 2024 Peak Scale
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

	// SucceededReason indicates a condition or event observed a success (Claim successful)
	SucceededReason string = "Claimed"

	// FailedReason indicates a condition or event observed a failure (Claim Rejected).
	FailedReason string = "Rejected"

	// ProgressingReason indicates a condition or event observed progression, for example when the reconciliation of a
	// resource or an action has started.
	ProgressingReason string = "Progressing"
)

// Can be used when tenant was successfully translated
// Should be used on translator level.
func NewReadyCondition(obj client.Object) metav1.Condition {
	return metav1.Condition{
		Type:               ReadyCondition,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             SucceededReason,
		Message:            "Claimed Resources",
		LastTransitionTime: metav1.Now(),
	}
}

func NewNotReadyCondition(obj client.Object, msg string) metav1.Condition {
	return metav1.Condition{
		Type:               NotReadyCondition,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: obj.GetGeneration(),
		Reason:             FailedReason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
}
