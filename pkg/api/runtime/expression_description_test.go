// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime_test

import (
	"testing"

	capsuleruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestDescribeExpressionMatch(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		match capsuleruntime.ExpressionMatch
		want  string
	}{
		{
			name:  "empty",
			match: capsuleruntime.ExpressionMatch{},
			want:  "",
		},
		{
			name:  "negated empty",
			match: capsuleruntime.ExpressionMatch{ExpressionRegex: capsuleruntime.ExpressionRegex{Negate: true}},
			want:  "not <empty>",
		},
		{
			name:  "exact",
			match: capsuleruntime.ExpressionMatch{Exact: []string{"a", "b"}},
			want:  "exact: a, b",
		},
		{
			name: "expression",
			match: capsuleruntime.ExpressionMatch{
				ExpressionRegex: capsuleruntime.ExpressionRegex{Expression: "^team-"},
			},
			want: "exp: ^team-",
		},
		{
			name: "exact and expression negated",
			match: capsuleruntime.ExpressionMatch{
				Exact:           []string{"a"},
				ExpressionRegex: capsuleruntime.ExpressionRegex{Expression: "^team-", Negate: true},
			},
			want: "not exact: a; not exp: ^team-",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := capsuleruntime.DescribeExpressionMatch(tt.match); got != tt.want {
				t.Fatalf("DescribeExpressionMatch() = %q, want %q", got, tt.want)
			}
			if got := tt.match.Describe(); got != tt.want {
				t.Fatalf("ExpressionMatch.Describe() = %q, want %q", got, tt.want)
			}
		})
	}
}
