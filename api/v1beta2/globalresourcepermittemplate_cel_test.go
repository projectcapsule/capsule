// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
)

func TestGlobalResourcePermitTemplateApprovalCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		conditions []string
		request    ResourcePermit
		want       bool
		wantError  string
	}{
		{name: "empty condition", want: true},
		{
			name:       "request reason",
			conditions: []string{`request.spec.reason == "incident"`},
			request:    ResourcePermit{Spec: ResourcePermitSpec{Reason: "incident"}},
			want:       true,
		},
		{
			name: "conditions are ORed",
			conditions: []string{
				`request.spec.reason == "maintenance"`,
				`requestor.name == "alice"`,
			},
			request: ResourcePermit{Spec: ResourcePermitSpec{
				Reason:    "incident",
				Requestor: resourcepermit.AccessEntity{Name: "alice"},
			}},
			want: true,
		},
		{
			name:       "requestor",
			conditions: []string{`requestor.name == "alice" && "developers" in requestor.groups`},
			request: ResourcePermit{Spec: ResourcePermitSpec{Requestor: resourcepermit.AccessEntity{
				Name: "alice", Groups: []string{"developers"},
			}}},
			want: true,
		},
		{
			name:       "reviewer group",
			conditions: []string{`"admin" in reviewer.groups`},
			request: ResourcePermit{Status: ResourcePermitStatus{Review: &ReviewInfo{Reviewer: &resourcepermit.AccessEntity{
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
			request: ResourcePermit{Spec: ResourcePermitSpec{
				Reason:    "maintenance",
				Requestor: resourcepermit.AccessEntity{Name: "bob"},
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
			request:    ResourcePermit{Spec: ResourcePermitSpec{Reason: "incident"}},
			wantError:  "must evaluate to bool",
		},
		{
			name: "matching condition wins over another evaluation error",
			conditions: []string{
				`request.spec.missing == "value"`,
				`request.spec.reason == "incident"`,
			},
			request: ResourcePermit{Spec: ResourcePermitSpec{Reason: "incident"}},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			brt := &GlobalResourcePermitTemplate{Spec: GlobalResourcePermitTemplateSpec{
				Approvals: resourcepermit.ApprovalSpec{Conditions: tt.conditions},
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

func TestResourcePermitUsesApprovalSnapshot(t *testing.T) {
	t.Parallel()

	template := &GlobalResourcePermitTemplate{Spec: GlobalResourcePermitTemplateSpec{
		Approvals: resourcepermit.ApprovalSpec{Conditions: []string{"false"}},
	}}
	request := &ResourcePermit{
		Status: ResourcePermitStatus{Request: &ResourcePermitStatusRequest{
			Approvals: &resourcepermit.ApprovalSpec{Conditions: []string{"true"}},
		}},
	}

	matched, err := request.EvaluateApprovalConditions(context.Background(), template)
	if err != nil {
		t.Fatalf("EvaluateApprovalConditions() error = %v", err)
	}
	if !matched {
		t.Fatal("EvaluateApprovalConditions() ignored the request approval snapshot")
	}

	request.Status.Request.Approvals = nil
	matched, err = request.EvaluateApprovalConditions(context.Background(), template)
	if err != nil {
		t.Fatalf("legacy EvaluateApprovalConditions() error = %v", err)
	}
	if matched {
		t.Fatal("EvaluateApprovalConditions() did not fall back to the template")
	}
}
