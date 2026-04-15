// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_hasNamespaceConditionTrue(t *testing.T) {
	t.Parallel()

	now := metav1.NewTime(time.Now())

	tests := []struct {
		name       string
		conditions []corev1.NamespaceCondition
		condType   corev1.NamespaceConditionType
		want       bool
	}{
		{
			name:       "no conditions",
			conditions: nil,
			condType:   corev1.NamespaceContentRemaining,
			want:       false,
		},
		{
			name: "type present but status false",
			conditions: []corev1.NamespaceCondition{
				{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionFalse, LastTransitionTime: now},
			},
			condType: corev1.NamespaceContentRemaining,
			want:     false,
		},
		{
			name: "type present with status true",
			conditions: []corev1.NamespaceCondition{
				{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionTrue, LastTransitionTime: now},
			},
			condType: corev1.NamespaceContentRemaining,
			want:     true,
		},
		{
			name: "different type true does not match",
			conditions: []corev1.NamespaceCondition{
				{Type: corev1.NamespaceFinalizersRemaining, Status: corev1.ConditionTrue, LastTransitionTime: now},
			},
			condType: corev1.NamespaceContentRemaining,
			want:     false,
		},
		{
			name: "multiple conditions, one matching true",
			conditions: []corev1.NamespaceCondition{
				{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionFalse, LastTransitionTime: now},
				{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionTrue, LastTransitionTime: now},
			},
			condType: corev1.NamespaceContentRemaining,
			want:     true,
		},
		{
			name: "multiple types, only requested type true counts",
			conditions: []corev1.NamespaceCondition{
				{Type: corev1.NamespaceContentRemaining, Status: corev1.ConditionFalse, LastTransitionTime: now},
				{Type: corev1.NamespaceFinalizersRemaining, Status: corev1.ConditionTrue, LastTransitionTime: now},
			},
			condType: corev1.NamespaceFinalizersRemaining,
			want:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ns := &corev1.Namespace{
				Status: corev1.NamespaceStatus{
					Conditions: tt.conditions,
				},
			}

			if got := hasNamespaceConditionTrue(ns, tt.condType); got != tt.want {
				t.Fatalf("hasNamespaceConditionTrue() = %v, want %v", got, tt.want)
			}
		})
	}
}
