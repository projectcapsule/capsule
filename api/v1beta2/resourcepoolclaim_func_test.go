// Copyright 2020-2025 Project Capsule Authors
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
						Type:   meta.BoundCondition,
						Status: metav1.ConditionTrue,
					},
				},
			},
			expected: true,
		},
		{
			name: "not bound - wrong condition type",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{
					Condition: metav1.Condition{
						Type:   "SomethingElse",
						Status: metav1.ConditionTrue,
					},
				},
			},
			expected: false,
		},
		{
			name: "not bound - status not true",
			claim: v1beta2.ResourcePoolClaim{
				Status: v1beta2.ResourcePoolClaimStatus{
					Condition: metav1.Condition{
						Type:   meta.BoundCondition,
						Status: metav1.ConditionFalse,
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
			actual := tt.claim.IsBoundToResourcePool()
			assert.Equal(t, tt.expected, actual)
		})
	}
}
