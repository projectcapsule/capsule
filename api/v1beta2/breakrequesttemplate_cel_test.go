// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

func TestBreakRequestTemplateApprovalCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		request    BreakRequest
		want       bool
		wantError  string
	}{
		{name: "empty condition", want: true},
		{
			name:       "request reason",
			expression: `request.spec.reason == "incident"`,
			request:    BreakRequest{Spec: BreakRequestSpec{Reason: "incident"}},
			want:       true,
		},
		{
			name:       "requestor",
			expression: `requestor.name == "alice" && "developers" in requestor.groups`,
			request: BreakRequest{Spec: BreakRequestSpec{Requestor: breaktheglass.AccessEntity{
				Name: "alice", Groups: []string{"developers"},
			}}},
			want: true,
		},
		{
			name:       "reviewer group",
			expression: `"admin" in reviewer.groups`,
			request: BreakRequest{Status: BreakRequestStatus{Review: &ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
				Name: "charlie", Groups: []string{"users", "admin"},
			}}}},
			want: true,
		},
		{
			name:       "nil reviewer has empty groups",
			expression: `"admin" in reviewer.groups`,
			want:       false,
		},
		{
			name:       "condition not met",
			expression: `request.spec.reason == "incident"`,
			request:    BreakRequest{Spec: BreakRequestSpec{Reason: "maintenance"}},
			want:       false,
		},
		{
			name:       "undefined variable",
			expression: `undefined_var == true`,
			wantError:  "undeclared reference",
		},
		{
			name:       "non boolean result",
			expression: `request.spec.reason`,
			request:    BreakRequest{Spec: BreakRequestSpec{Reason: "incident"}},
			wantError:  "must evaluate to bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			brt := &BreakRequestTemplate{Spec: BreakRequestTemplateSpec{ApprovalCondition: tt.expression}}
			got, err := brt.EvaluateApprovalCondition(context.Background(), &tt.request)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("EvaluateApprovalCondition() error = %v, want containing %q", err, tt.wantError)
				}

				return
			}
			if err != nil {
				t.Fatalf("EvaluateApprovalCondition() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EvaluateApprovalCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
