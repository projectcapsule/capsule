// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestEnforceBodiesFromNamespaceRules(t *testing.T) {
	tests := []struct {
		name   string
		input  []*api.NamespaceRuleBodyNamespace
		assert func(t *testing.T, got []*api.NamespaceRuleEnforceBody)
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if got != nil {
					t.Fatalf("expected nil, got %#v", got)
				}
			},
		},
		{
			name:  "empty input returns nil",
			input: []*api.NamespaceRuleBodyNamespace{},
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if got != nil {
					t.Fatalf("expected nil, got %#v", got)
				}
			},
		},
		{
			name: "only nil bodies returns empty slice",
			input: []*api.NamespaceRuleBodyNamespace{
				nil,
				nil,
			},
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if got == nil {
					t.Fatalf("expected non-nil empty slice, got nil")
				}

				if len(got) != 0 {
					t.Fatalf("expected empty slice, got len=%d", len(got))
				}
			},
		},
		{
			name: "bodies without enforce are skipped",
			input: []*api.NamespaceRuleBodyNamespace{
				{},
				{
					Enforce: nil,
				},
			},
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if got == nil {
					t.Fatalf("expected non-nil empty slice, got nil")
				}

				if len(got) != 0 {
					t.Fatalf("expected empty slice, got len=%d", len(got))
				}
			},
		},
		{
			name: "returns enforce bodies in original order",
			input: func() []*api.NamespaceRuleBodyNamespace {
				first := &api.NamespaceRuleEnforceBody{
					Action: api.ActionTypeAllow,
				}
				second := &api.NamespaceRuleEnforceBody{
					Action: api.ActionTypeDeny,
				}
				third := &api.NamespaceRuleEnforceBody{
					Action: api.ActionTypeAudit,
				}

				return []*api.NamespaceRuleBodyNamespace{
					{
						Enforce: first,
					},
					nil,
					{
						Enforce: second,
					},
					{},
					{
						Enforce: third,
					},
				}
			}(),
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if len(got) != 3 {
					t.Fatalf("expected 3 enforce bodies, got %d", len(got))
				}

				if got[0].Action != api.ActionTypeAllow {
					t.Fatalf("expected first action %q, got %q", api.ActionTypeAllow, got[0].Action)
				}

				if got[1].Action != api.ActionTypeDeny {
					t.Fatalf("expected second action %q, got %q", api.ActionTypeDeny, got[1].Action)
				}

				if got[2].Action != api.ActionTypeAudit {
					t.Fatalf("expected third action %q, got %q", api.ActionTypeAudit, got[2].Action)
				}
			},
		},
		{
			name: "returns original enforce pointers without deep copy",
			input: func() []*api.NamespaceRuleBodyNamespace {
				enforce := &api.NamespaceRuleEnforceBody{
					Action: api.ActionTypeAllow,
				}

				return []*api.NamespaceRuleBodyNamespace{
					{
						Enforce: enforce,
					},
				}
			}(),
			assert: func(t *testing.T, got []*api.NamespaceRuleEnforceBody) {
				t.Helper()

				if len(got) != 1 {
					t.Fatalf("expected one enforce body, got %d", len(got))
				}

				if got[0].Action != api.ActionTypeAllow {
					t.Fatalf("expected action %q, got %q", api.ActionTypeAllow, got[0].Action)
				}

				got[0].Action = api.ActionTypeDeny

				if got[0].Action != api.ActionTypeDeny {
					t.Fatalf("expected returned pointer to be mutable")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforceBodiesFromNamespaceRules(tt.input)
			tt.assert(t, got)
		})
	}
}

func TestEnforceBodiesFromNamespaceRulesReturnsOriginalPointers(t *testing.T) {
	first := &api.NamespaceRuleEnforceBody{
		Action: api.ActionTypeAllow,
	}
	second := &api.NamespaceRuleEnforceBody{
		Action: api.ActionTypeDeny,
	}

	got := EnforceBodiesFromNamespaceRules([]*api.NamespaceRuleBodyNamespace{
		{
			Enforce: first,
		},
		{
			Enforce: second,
		},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 enforce bodies, got %d", len(got))
	}

	if got[0] != first {
		t.Fatalf("expected first returned enforce body to be the original pointer")
	}

	if got[1] != second {
		t.Fatalf("expected second returned enforce body to be the original pointer")
	}
}
