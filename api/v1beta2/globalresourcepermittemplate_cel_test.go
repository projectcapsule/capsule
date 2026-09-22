// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
)

func TestApprovalRequesterCompatibility(t *testing.T) {
	for _, expression := range []string{
		`requestor.name == "alice" && "developers" in requestor.groups`,
		`requester.name == "alice" && "developers" in requester.groups`,
		`requestor == requester && requester.name == "alice" && "developers" in requestor.groups`,
	} {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("global=%t/%s", global, expression), func(t *testing.T) {
				approvals := resourcepermit.ApprovalSpec{Conditions: []string{expression}}
				var template ResourcePermitTemplateSource = &ResourcePermitTemplate{Spec: ResourcePermitTemplateSpec{Approvals: approvals}}
				if global {
					template = &GlobalResourcePermitTemplate{Spec: GlobalResourcePermitTemplateSpec{Approvals: approvals}}
				}
				require.NoError(t, template.ValidateApprovalConditions())
				for _, actor := range []resourcepermit.AccessEntity{
					{Name: "alice", Groups: []string{"developers"}},
					{Name: "bob", Groups: []string{"developers"}},
					{Name: "alice"},
				} {
					request := &ResourcePermit{Spec: ResourcePermitSpec{Requester: actor}}
					want := actor.Name == "alice" && len(actor.Groups) > 0
					matched, err := template.EvaluateApprovalConditions(t.Context(), request)
					require.NoError(t, err)
					require.Equal(t, want, matched)
					// Existing approval snapshots must keep working even after the
					// source template changes or disappears.
					request.Status.Request = &ResourcePermitStatusRequest{Approvals: &approvals}
					matched, err = request.EvaluateApprovalConditions(t.Context(), nil)
					require.NoError(t, err)
					require.Equal(t, want, matched)
				}
			})
		}
	}
}

func BenchmarkApprovalConditions(b *testing.B) {
	for _, count := range []int{1, 8} {
		for _, evaluate := range []bool{false, true} {
			b.Run(fmt.Sprintf("conditions=%d/evaluate=%t", count, evaluate), func(b *testing.B) {
				conditions := make([]string, count)
				for i := range conditions {
					conditions[i] = fmt.Sprintf(`requester.name == "user-%d" && "developers" in requester.groups`, i)
				}
				template := &GlobalResourcePermitTemplate{Spec: GlobalResourcePermitTemplateSpec{Approvals: resourcepermit.ApprovalSpec{Conditions: conditions}}}
				request := &ResourcePermit{Spec: ResourcePermitSpec{Requester: resourcepermit.AccessEntity{Name: fmt.Sprintf("user-%d", count-1), Groups: []string{"developers"}}}}
				b.ReportAllocs()
				for b.Loop() {
					if evaluate {
						matched, err := template.EvaluateApprovalConditions(b.Context(), request)
						if err != nil || !matched {
							b.Fatalf("approval evaluation: matched=%t err=%v", matched, err)
						}
					} else if err := template.ValidateApprovalConditions(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

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
				`requester.name == "alice"`,
			},
			request: ResourcePermit{Spec: ResourcePermitSpec{
				Reason:    "incident",
				Requester: resourcepermit.AccessEntity{Name: "alice"},
			}},
			want: true,
		},
		{
			name:       "requester",
			conditions: []string{`requester.name == "alice" && "developers" in requester.groups`},
			request: ResourcePermit{Spec: ResourcePermitSpec{Requester: resourcepermit.AccessEntity{
				Name: "alice", Groups: []string{"developers"},
			}}},
			want: true,
		},
		{
			name:       "request spec requester matches the CEL identity",
			conditions: []string{`request.spec.requester.name == requester.name && request.spec.requester.groups == requester.groups`},
			request: ResourcePermit{Spec: ResourcePermitSpec{Requester: resourcepermit.AccessEntity{
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
			conditions: []string{`request.spec.reason == "incident"`, `requester.name == "alice"`},
			request: ResourcePermit{Spec: ResourcePermitSpec{
				Reason:    "maintenance",
				Requester: resourcepermit.AccessEntity{Name: "bob"},
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
