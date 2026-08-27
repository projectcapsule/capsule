// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name        string
		spec        capsulev1beta2.BreakRequestTemplateSpec
		br          capsulev1beta2.BreakRequest
		expectError string
	}{
		{
			name: "Approved if no condition",
			spec: capsulev1beta2.BreakRequestTemplateSpec{ApprovalCondition: ""},
			br:   capsulev1beta2.BreakRequest{},
		},
		{
			name: "Reason is correct",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "request.spec.reason == 'test'",
			},
			br: capsulev1beta2.BreakRequest{Spec: capsulev1beta2.BreakRequestSpec{Reason: "test"}},
		},
		{
			name: "Requestor name is correct",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "requestor.name == 'alice'",
			},
			br: capsulev1beta2.BreakRequest{
				Spec: capsulev1beta2.BreakRequestSpec{
					Requestor: breaktheglass.AccessEntity{Name: "alice"},
				},
			},
		},
		{
			name: "Reviewer group is correct",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "'admin' in reviewer.groups",
			},
			br: capsulev1beta2.BreakRequest{
				Status: capsulev1beta2.BreakRequestStatus{
					Review: &capsulev1beta2.ReviewInfo{
						Reviewer: &breaktheglass.AccessEntity{
							Groups: []string{"admin", "users"},
						},
					},
				},
			},
		},
		{
			name: "Requestor service account type is correct",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "requestor.type == 'ServiceAccount'",
			},
			br: capsulev1beta2.BreakRequest{
				Spec: capsulev1beta2.BreakRequestSpec{
					Requestor: breaktheglass.AccessEntity{
						Name: "system:serviceaccount:ns:sa",
						Type: breaktheglass.AccessEntityTypeServiceAccount,
					},
				},
			},
		},
		{
			name: "Condition not met",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "request.spec.reason == 'test'",
			},
			br:          capsulev1beta2.BreakRequest{Spec: capsulev1beta2.BreakRequestSpec{Reason: "not-test"}},
			expectError: "approval condition (request.spec.reason == 'test') not met",
		},
		{
			name: "Syntax error in CEL",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "request.spec.reason ==",
			},
			br:          capsulev1beta2.BreakRequest{},
			expectError: "failed to compile CEL expression",
		},
		{
			name: "Non-boolean result",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "request.spec.reason",
			},
			br:          capsulev1beta2.BreakRequest{Spec: capsulev1beta2.BreakRequestSpec{Reason: "test"}},
			expectError: "approval condition (request.spec.reason) did not evaluate to a boolean",
		},
		{
			name: "Reviewer is nil",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "'admin' in reviewer.groups",
			},
			br:          capsulev1beta2.BreakRequest{},
			expectError: "runtime error evaluating approval condition ('admin' in reviewer.groups): no such key: groups",
		},
		{
			name: "Undefined variable",
			spec: capsulev1beta2.BreakRequestTemplateSpec{
				ApprovalCondition: "undefined_var == true",
			},
			br:          capsulev1beta2.BreakRequest{},
			expectError: "undeclared reference to 'undefined_var'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brt := &capsulev1beta2.BreakRequestTemplate{Spec: tt.spec}
			err := IsAllowed(brt, &tt.br)
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
