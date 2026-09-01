// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

func TestGlobalBreakRequestTemplateApprovalCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		conditions []string
		request    BreakRequest
		want       bool
		wantError  string
	}{
		{name: "empty condition", want: true},
		{
			name:       "request reason",
			conditions: []string{`request.spec.reason == "incident"`},
			request:    BreakRequest{Spec: BreakRequestSpec{Reason: "incident"}},
			want:       true,
		},
		{
			name: "conditions are ORed",
			conditions: []string{
				`request.spec.reason == "maintenance"`,
				`requestor.name == "alice"`,
			},
			request: BreakRequest{Spec: BreakRequestSpec{
				Reason:    "incident",
				Requestor: breaktheglass.AccessEntity{Name: "alice"},
			}},
			want: true,
		},
		{
			name:       "requestor",
			conditions: []string{`requestor.name == "alice" && "developers" in requestor.groups`},
			request: BreakRequest{Spec: BreakRequestSpec{Requestor: breaktheglass.AccessEntity{
				Name: "alice", Groups: []string{"developers"},
			}}},
			want: true,
		},
		{
			name:       "reviewer group",
			conditions: []string{`"admin" in reviewer.groups`},
			request: BreakRequest{Status: BreakRequestStatus{Review: &ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
				Name: "charlie", Groups: []string{"users", "admin"},
			}}}},
			want: true,
		},
		{
			name:       "nil reviewer has empty groups",
			conditions: []string{`"admin" in reviewer.groups`},
			want:       false,
		},
		{
			name:       "no condition met",
			conditions: []string{`request.spec.reason == "incident"`, `requestor.name == "alice"`},
			request: BreakRequest{Spec: BreakRequestSpec{
				Reason:    "maintenance",
				Requestor: breaktheglass.AccessEntity{Name: "bob"},
			}},
			want: false,
		},
		{
			name:       "undefined variable",
			conditions: []string{`undefined_var == true`},
			wantError:  "undeclared reference",
		},
		{
			name:       "non boolean result",
			conditions: []string{`request.spec.reason`},
			request:    BreakRequest{Spec: BreakRequestSpec{Reason: "incident"}},
			wantError:  "must evaluate to bool",
		},
		{
			name: "matching condition wins over another evaluation error",
			conditions: []string{
				`request.spec.missing == "value"`,
				`request.spec.reason == "incident"`,
			},
			request: BreakRequest{Spec: BreakRequestSpec{Reason: "incident"}},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			brt := &GlobalBreakRequestTemplate{Spec: GlobalBreakRequestTemplateSpec{
				Approvals: breaktheglass.ApprovalSpec{Conditions: tt.conditions},
			}}
			got, err := brt.EvaluateApprovalConditions(context.Background(), &tt.request)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("EvaluateApprovalConditions() error = %v, want containing %q", err, tt.wantError)
				}

				return
			}
			if err != nil {
				t.Fatalf("EvaluateApprovalConditions() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EvaluateApprovalConditions() = %v, want %v", got, tt.want)
			}
		})
	}
}
