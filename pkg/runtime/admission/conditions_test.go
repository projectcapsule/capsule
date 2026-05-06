// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/admission"
)

const serviceAccountGuard = "request.userInfo.groups.exists(g, g == 'system:serviceaccounts')"

func TestCelQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "''"},
		{"plain", "abc", "'abc'"},
		{"single_quote", "a'b", "'a\\'b'"},
		{"backslash", `a\b`, "'a\\\\b'"},
		{"newline", "a\nb", "'a\\nb'"},
		{"tab", "a\tb", "'a\\tb'"},
		{"combo", "a\\b'c\nd\te", "'a\\\\b\\'c\\nd\\te'"},
		// Ensure already-escaped sequences are not double-escaped in an unexpected way:
		{"literal_backslash_n", `a\nb`, "'a\\\\nb'"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := admission.CelQuote(tt.in); got != tt.want {
				t.Fatalf("CelQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCelStringList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"nil", nil, "[]"},
		{"empty", []string{}, "[]"},
		{"single", []string{"alice"}, "['alice']"},
		{"two", []string{"alice", "bob"}, "['alice','bob']"},
		{"needs_escape", []string{`a\b`, "x'y", "n\n", "t\t"}, "['a\\\\b','x\\'y','n\\n','t\\t']"},
		{"preserve_order", []string{"b", "a"}, "['b','a']"}, // caller controls sorting
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := admission.CelStringList(tt.in); got != tt.want {
				t.Fatalf("CelStringList(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCelUserOrGroupExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   rbac.UserListSpec
		want string
	}{
		{
			name: "empty_list",
			in:   nil,
			want: "(false) || (false)",
		},
		{
			name: "users_only",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.UserOwner, Name: "bob"},
			},
			// NOTE: SplitUsersAndGroups sorts => alice,bob
			want: "(request.userInfo.username in ['alice','bob']) || (false)",
		},
		{
			name: "groups_only",
			in: rbac.UserListSpec{
				{Kind: rbac.GroupOwner, Name: "dev"},
				{Kind: rbac.GroupOwner, Name: "ops"},
			},
			// NOTE: sorted dev,ops
			want: "(false) || (request.userInfo.groups.exists(g, g in ['dev','ops']))",
		},
		{
			name: "users_and_groups",
			in: rbac.UserListSpec{
				{Kind: rbac.GroupOwner, Name: "team-b"},
				{Kind: rbac.UserOwner, Name: "zara"},
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.GroupOwner, Name: "team-a"},
			},
			want: "(request.userInfo.username in ['alice','zara']) || (request.userInfo.groups.exists(g, g in ['team-a','team-b']))",
		},
		{
			name: "serviceaccounts_are_users_bucket",
			in: rbac.UserListSpec{
				{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:ns:sa"},
			},
			want: "(request.userInfo.username in ['system:serviceaccount:ns:sa']) || (false)",
		},
		{
			name: "escaping_in_names",
			in: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "a\\b"},
				{Kind: rbac.GroupOwner, Name: "x'y"},
			},
			// users sorted: a\b ; groups: x'y
			want: "(request.userInfo.username in ['a\\\\b']) || (request.userInfo.groups.exists(g, g in ['x\\'y']))",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := admission.CelUserOrGroupExpr(tt.in); got != tt.want {
				t.Fatalf("CelUserOrGroupExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildGatingUserCondition(t *testing.T) {
	t.Parallel()

	users := rbac.UserListSpec{
		{Kind: rbac.UserOwner, Name: "alice"},
		{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"},
	}
	admins := rbac.UserListSpec{
		{Kind: rbac.UserOwner, Name: "root"},
		{Kind: rbac.GroupOwner, Name: "system:masters"},
	}

	tests := []struct {
		name     string
		opts     admission.WebhookOptions
		wantNil  bool
		wantExpr string
	}{
		{
			name:    "no_options_enabled_returns_nil",
			opts:    admission.WebhookOptions{},
			wantNil: true,
		},
		{
			name: "capsule_users_only",
			opts: admission.WebhookOptions{CapsuleUsers: true},
			wantExpr: "(" + serviceAccountGuard + ") || " +
				"((request.userInfo.username in ['alice']) || (request.userInfo.groups.exists(g, g in ['projectcapsule.dev'])))",
		},
		{
			name:     "administrators_only",
			opts:     admission.WebhookOptions{Administrators: true},
			wantExpr: "(request.userInfo.username in ['root']) || (request.userInfo.groups.exists(g, g in ['system:masters']))",
		},
		{
			name: "both_enabled_or_combined",
			opts: admission.WebhookOptions{CapsuleUsers: true, Administrators: true},
			wantExpr: "((" + serviceAccountGuard + ") || " +
				"((request.userInfo.username in ['alice']) || (request.userInfo.groups.exists(g, g in ['projectcapsule.dev'])))) || " +
				"((request.userInfo.username in ['root']) || (request.userInfo.groups.exists(g, g in ['system:masters'])))",
		},
		{
			name:     "capsule_users_enabled_but_empty_list",
			opts:     admission.WebhookOptions{CapsuleUsers: true},
			wantExpr: "(" + serviceAccountGuard + ") || ((false) || (false))",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u := users
			a := admins
			if tt.name == "capsule_users_enabled_but_empty_list" {
				u = nil
				a = nil
			}

			got := admission.BuildGatingUserCondition(tt.opts, u, a)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %#v", got)
				}
				return
			}

			if got == nil || len(got) != 1 {
				t.Fatalf("expected exactly 1 MatchCondition, got %#v", got)
			}
			if got[0].Name != "capsule-user-gate" {
				t.Fatalf("MatchCondition.Name = %q, want %q", got[0].Name, "capsule-user-gate")
			}
			if got[0].Expression != tt.wantExpr {
				t.Fatalf("MatchCondition.Expression = %q, want %q", got[0].Expression, tt.wantExpr)
			}
		})
	}
}
