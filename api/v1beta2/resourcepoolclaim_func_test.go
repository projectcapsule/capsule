// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestIsBoundToResourcePool(t *testing.T) {
	tests := []struct {
		name     string
		claim    v1beta2.ResourcePoolClaim
		expected bool
	}{
		{
			name: "bound to resource pool (Assigned=True)",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{
					Condition: metav1.Condition{
						Type:               meta.BoundCondition,
						Status:             metav1.ConditionTrue,
						Reason:             meta.SucceededReason,
						Message:            "reconciled",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		},
		{
			name: "not bound - wrong condition type",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{
					Conditions: meta.ConditionList{
						meta.Condition{
							Type:               meta.AssignedCondition,
							Status:             metav1.ConditionTrue,
							Reason:             meta.SucceededReason,
							Message:            "reconciled",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "not bound - status not true",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{
					Conditions: meta.ConditionList{
						meta.Condition{
							Type:               meta.AssignedCondition,
							Status:             metav1.ConditionTrue,
							Reason:             meta.SucceededReason,
							Message:            "reconciled",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "not bound - empty condition",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.claim.IsBoundInResourcePool()
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestGetPool(t *testing.T) {
	tests := []struct {
		name     string
		claim    v1beta2.ResourcePoolClaim
		expected string
	}{
		{
			name: "returns status pool name when set",
			claim: v1beta2.ResourcePoolClaim{
				Spec: v1beta2.ResourcePoolClaimSpec{
					Pool: "spec-pool",
				},
				Status: v1beta2.ResourcePoolClaimStatus{
					Pool: meta.LocalRFC1123ObjectReferenceWithUID{
						Name: meta.RFC1123Name("status-pool"),
					},
				},
			},
			expected: "status-pool",
		},
		{
			name: "falls back to spec pool when status pool name is empty",
			claim: v1beta2.ResourcePoolClaim{
				Spec: v1beta2.ResourcePoolClaimSpec{
					Pool: "spec-pool",
				},
				Status: v1beta2.ResourcePoolClaimStatus{
					Pool: meta.LocalRFC1123ObjectReferenceWithUID{
						Name: meta.RFC1123Name(""),
					},
				},
			},
			expected: "spec-pool",
		},
		{
			name: "falls back to spec pool when status pool struct is zero-value",
			claim: v1beta2.ResourcePoolClaim{
				Spec: v1beta2.ResourcePoolClaimSpec{
					Pool: "spec-pool",
				},
				Status: v1beta2.ResourcePoolClaimStatus{
					Pool: meta.LocalRFC1123ObjectReferenceWithUID{},
				},
			},
			expected: "spec-pool",
		},
		{
			name: "returns empty when both status and spec are empty",
			claim: v1beta2.ResourcePoolClaim{
				Spec: v1beta2.ResourcePoolClaimSpec{
					Pool: "",
				},
				Status: v1beta2.ResourcePoolClaimStatus{
					Pool: meta.LocalRFC1123ObjectReferenceWithUID{
						Name: meta.RFC1123Name(""),
					},
				},
			},
			expected: "",
		},
		{
			name: "status wins even if spec differs",
			claim: v1beta2.ResourcePoolClaim{
				Spec: v1beta2.ResourcePoolClaimSpec{
					Pool: "spec-pool",
				},
				Status: v1beta2.ResourcePoolClaimStatus{
					Pool: meta.LocalRFC1123ObjectReferenceWithUID{
						Name: meta.RFC1123Name("status-pool"),
					},
				},
			},
			expected: "status-pool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.claim.GetPool()
			assert.Equal(t, tt.expected, actual)
		})
	}
}
