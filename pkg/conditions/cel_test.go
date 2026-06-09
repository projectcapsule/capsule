// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsApproved(t *testing.T) {
	tests := []struct {
		name        string
		spec        capsulev1beta2.BreakRequestTemplateSpec
		br          capsulev1beta2.BreakRequest
		approved    bool
		expectError bool
	}{
		{
			name:        "Not approved if no auto approval case 1",
			spec:        capsulev1beta2.BreakRequestTemplateSpec{AutoApprove: false},
			br:          capsulev1beta2.BreakRequest{},
			approved:    false,
			expectError: false,
		},
		{
			name:        "Approved if auto approval and no condition",
			spec:        capsulev1beta2.BreakRequestTemplateSpec{AutoApprove: true, ApprovalCondition: ""},
			br:          capsulev1beta2.BreakRequest{},
			approved:    true,
			expectError: false,
		},
		{
			name: "Reason is correct",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				AutoApprove:       true,
				ApprovalCondition: "request.spec.reason == 'test'",
			},
			br:          capsulev1beta2.BreakRequest{Spec: capsulev1beta2.BreakRequestSpec{Reason: "test"}},
			approved:    true,
			expectError: false,
		},
		{
			name: "Reason is incorrect",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				AutoApprove:       true,
				ApprovalCondition: "request.spec.reason == 'test'",
			},
			br:          capsulev1beta2.BreakRequest{Spec: capsulev1beta2.BreakRequestSpec{Reason: "TEST"}},
			approved:    false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brt := &capsulev1beta2.BreakRequestTemplate{Spec: tt.spec}
			result, err := IsApproved(brt, &tt.br)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.approved, result)
			}
		})
	}
}
