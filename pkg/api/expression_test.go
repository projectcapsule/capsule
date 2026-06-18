// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
)

type fakeExpressionRegexMatcher struct {
	t *testing.T

	calls int

	err error

	matches map[string]bool
	seen    []ExpressionRegex
}

func (m *fakeExpressionRegexMatcher) MatchRegex(expr ExpressionRegex, value string) (bool, error) {
	m.t.Helper()

	m.calls++
	m.seen = append(m.seen, expr)

	if m.err != nil {
		return false, m.err
	}

	key := fmt.Sprintf("%s|%t|%s", expr.Expression, expr.Negate, value)
	if matched, ok := m.matches[key]; ok {
		return matched, nil
	}

	re, err := regexp.Compile(expr.Expression)
	if err != nil {
		return false, err
	}

	matched := re.MatchString(value)
	if expr.Negate {
		return !matched, nil
	}

	return matched, nil
}

func TestExpressionMatch_Matches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		match     ExpressionMatch
		value     string
		wantMatch bool
		wantErr   bool
	}{
		{
			name: "exact matches single value",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
			},
			value:     "default-scheduler",
			wantMatch: true,
		},
		{
			name: "exact does not match different value",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
			},
			value:     "custom-scheduler",
			wantMatch: false,
		},
		{
			name: "exact matches one of multiple values",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler", "custom-scheduler", "team-scheduler"},
			},
			value:     "custom-scheduler",
			wantMatch: true,
		},
		{
			name: "exact does not match any of multiple values",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler", "custom-scheduler"},
			},
			value:     "other-scheduler",
			wantMatch: false,
		},
		{
			name: "exact is case sensitive",
			match: ExpressionMatch{
				Exact: []string{"Default-Scheduler"},
			},
			value:     "default-scheduler",
			wantMatch: false,
		},
		{
			name: "exact uses literal string not pattern",
			match: ExpressionMatch{
				Exact: []string{"team-.*"},
			},
			value:     "team-a",
			wantMatch: false,
		},
		{
			name: "exact matches literal pattern string",
			match: ExpressionMatch{
				Exact: []string{"team-.*"},
			},
			value:     "team-.*",
			wantMatch: true,
		},
		{
			name: "exact with empty value can match empty string when present",
			match: ExpressionMatch{
				Exact: []string{""},
			},
			value:     "",
			wantMatch: true,
		},
		{
			name: "regex matches value",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-[a-z0-9-]+$",
				},
			},
			value:     "team-alpha-1",
			wantMatch: true,
		},
		{
			name: "regex does not match value",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-[a-z0-9-]+$",
				},
			},
			value:     "kube-scheduler",
			wantMatch: false,
		},
		{
			name: "regex is not implicitly anchored",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "team",
				},
			},
			value:     "my-team-scheduler",
			wantMatch: true,
		},
		{
			name: "invalid regex returns error",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "[",
				},
			},
			value:   "team-alpha",
			wantErr: true,
		},
		{
			name: "combined exact and regex matches by exact",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-[a-z0-9-]+$",
				},
			},
			value:     "default-scheduler",
			wantMatch: true,
		},
		{
			name: "combined exact and regex matches by regex",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-[a-z0-9-]+$",
				},
			},
			value:     "team-alpha",
			wantMatch: true,
		},
		{
			name: "combined exact and regex does not match either",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-[a-z0-9-]+$",
				},
			},
			value:     "other-scheduler",
			wantMatch: false,
		},
		{
			name: "combined exact match skips invalid regex",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Expression: "[",
				},
			},
			value:     "default-scheduler",
			wantMatch: true,
		},
		{
			name: "combined exact miss evaluates invalid regex and returns error",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Expression: "[",
				},
			},
			value:   "team-alpha",
			wantErr: true,
		},
		{
			name: "negated exact matching value returns false",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			value:     "default-scheduler",
			wantMatch: false,
		},
		{
			name: "negated exact non matching value returns true",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			value:     "custom-scheduler",
			wantMatch: true,
		},
		{
			name: "negated exact with multiple values matching one returns false",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler", "custom-scheduler"},
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			value:     "custom-scheduler",
			wantMatch: false,
		},
		{
			name: "negated regex matching value returns false",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.*",
					Negate:     true,
				},
			},
			value:     "trusted/platform/app:1",
			wantMatch: false,
		},
		{
			name: "negated regex non matching value returns true",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.*",
					Negate:     true,
				},
			},
			value:     "docker.io/library/nginx:latest",
			wantMatch: true,
		},
		{
			name: "negated combined exact match returns false",
			match: ExpressionMatch{
				Exact: []string{"trusted/platform/app:1"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.+",
					Negate:     true,
				},
			},
			value:     "trusted/platform/app:1",
			wantMatch: false,
		},
		{
			name: "negated combined regex match returns false",
			match: ExpressionMatch{
				Exact: []string{"trusted/platform/app:1"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.+",
					Negate:     true,
				},
			},
			value:     "trusted/other/app:1",
			wantMatch: false,
		},
		{
			name: "negated combined no match returns true",
			match: ExpressionMatch{
				Exact: []string{"trusted/platform/app:1"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.+",
					Negate:     true,
				},
			},
			value:     "harbor/platform/app:1",
			wantMatch: true,
		},
		{
			name:      "empty matcher returns error",
			match:     ExpressionMatch{},
			value:     "anything",
			wantErr:   true,
			wantMatch: false,
		},
		{
			name: "empty exact slice with empty regex returns error",
			match: ExpressionMatch{
				Exact: []string{},
			},
			value:   "anything",
			wantErr: true,
		},
		{
			name: "nil exact with whitespace regex is treated as regex and does not match",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: " ",
				},
			},
			value:     "anything",
			wantMatch: false,
		},
		{
			name: "duplicate exact values still match",
			match: ExpressionMatch{
				Exact: []string{"a", "a", "b"},
			},
			value:     "a",
			wantMatch: true,
		},
		{
			name: "exact values are not trimmed",
			match: ExpressionMatch{
				Exact: []string{" value "},
			},
			value:     "value",
			wantMatch: false,
		},
		{
			name: "exact values match with spaces when value has spaces",
			match: ExpressionMatch{
				Exact: []string{" value "},
			},
			value:     " value ",
			wantMatch: true,
		},
		{
			name: "regex can match empty value",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^$",
				},
			},
			value:     "",
			wantMatch: true,
		},
		{
			name: "negated regex can reject empty value",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^$",
					Negate:     true,
				},
			},
			value:     "",
			wantMatch: false,
		},
		{
			name: "negated regex can match non empty value against empty regex",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^$",
					Negate:     true,
				},
			},
			value:     "non-empty",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.match.Matches(tt.value)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Matches() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Matches() unexpected error: %v", err)
			}

			if got != tt.wantMatch {
				t.Fatalf("Matches() = %t, want %t", got, tt.wantMatch)
			}
		})
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_NilMatcherFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		match     ExpressionMatch
		value     string
		wantMatch bool
		wantErr   bool
	}{
		{
			name: "nil matcher exact match",
			match: ExpressionMatch{
				Exact: []string{"default-scheduler"},
			},
			value:     "default-scheduler",
			wantMatch: true,
		},
		{
			name: "nil matcher regex match",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^team-.*",
				},
			},
			value:     "team-a",
			wantMatch: true,
		},
		{
			name: "nil matcher negated regex non match returns true",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^trusted/.*",
					Negate:     true,
				},
			},
			value:     "docker.io/library/nginx:latest",
			wantMatch: true,
		},
		{
			name: "nil matcher invalid regex returns error",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "[",
				},
			},
			value:   "team-a",
			wantErr: true,
		},
		{
			name:    "nil matcher empty expression match returns error",
			match:   ExpressionMatch{},
			value:   "team-a",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.match.MatchesWithExpressionMatcher(nil, tt.value)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("MatchesWithExpressionMatcher(nil) expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("MatchesWithExpressionMatcher(nil) unexpected error: %v", err)
			}

			if got != tt.wantMatch {
				t.Fatalf("MatchesWithExpressionMatcher(nil) = %t, want %t", got, tt.wantMatch)
			}
		})
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_UsesMatcherForRegex(t *testing.T) {
	t.Parallel()

	matcher := &fakeExpressionRegexMatcher{
		t: t,
		matches: map[string]bool{
			"^team-.*|false|team-a": true,
		},
	}

	match := ExpressionMatch{
		ExpressionRegex: ExpressionRegex{
			Expression: "^team-.*",
		},
	}

	got, err := match.MatchesWithExpressionMatcher(matcher, "team-a")
	if err != nil {
		t.Fatalf("MatchesWithExpressionMatcher() unexpected error: %v", err)
	}

	if !got {
		t.Fatalf("MatchesWithExpressionMatcher() = false, want true")
	}

	if matcher.calls != 1 {
		t.Fatalf("MatchRegex() calls = %d, want 1", matcher.calls)
	}

	if len(matcher.seen) != 1 {
		t.Fatalf("seen expressions = %d, want 1", len(matcher.seen))
	}

	if matcher.seen[0].Expression != "^team-.*" {
		t.Fatalf("seen expression = %q, want %q", matcher.seen[0].Expression, "^team-.*")
	}

	if matcher.seen[0].Negate {
		t.Fatalf("seen negate = true, want false")
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_PassesNegateToMatcher(t *testing.T) {
	t.Parallel()

	matcher := &fakeExpressionRegexMatcher{
		t: t,
		matches: map[string]bool{
			"^trusted/.*|true|docker.io/library/nginx:latest": true,
		},
	}

	match := ExpressionMatch{
		ExpressionRegex: ExpressionRegex{
			Expression: "^trusted/.*",
			Negate:     true,
		},
	}

	got, err := match.MatchesWithExpressionMatcher(matcher, "docker.io/library/nginx:latest")
	if err != nil {
		t.Fatalf("MatchesWithExpressionMatcher() unexpected error: %v", err)
	}

	if !got {
		t.Fatalf("MatchesWithExpressionMatcher() = false, want true")
	}

	if matcher.calls != 1 {
		t.Fatalf("MatchRegex() calls = %d, want 1", matcher.calls)
	}

	if len(matcher.seen) != 1 {
		t.Fatalf("seen expressions = %d, want 1", len(matcher.seen))
	}

	if !matcher.seen[0].Negate {
		t.Fatalf("seen negate = false, want true")
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_DoesNotUseMatcherWhenExactMatches(t *testing.T) {
	t.Parallel()

	matcher := &fakeExpressionRegexMatcher{
		t:   t,
		err: errors.New("matcher should not be called"),
	}

	match := ExpressionMatch{
		Exact: []string{"default-scheduler"},
		ExpressionRegex: ExpressionRegex{
			Expression: "[",
		},
	}

	got, err := match.MatchesWithExpressionMatcher(matcher, "default-scheduler")
	if err != nil {
		t.Fatalf("MatchesWithExpressionMatcher() unexpected error: %v", err)
	}

	if !got {
		t.Fatalf("MatchesWithExpressionMatcher() = false, want true")
	}

	if matcher.calls != 0 {
		t.Fatalf("MatchRegex() calls = %d, want 0", matcher.calls)
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_UsesMatcherWhenExactDoesNotMatch(t *testing.T) {
	t.Parallel()

	matcher := &fakeExpressionRegexMatcher{
		t: t,
		matches: map[string]bool{
			"^team-.*|false|team-a": true,
		},
	}

	match := ExpressionMatch{
		Exact: []string{"default-scheduler"},
		ExpressionRegex: ExpressionRegex{
			Expression: "^team-.*",
		},
	}

	got, err := match.MatchesWithExpressionMatcher(matcher, "team-a")
	if err != nil {
		t.Fatalf("MatchesWithExpressionMatcher() unexpected error: %v", err)
	}

	if !got {
		t.Fatalf("MatchesWithExpressionMatcher() = false, want true")
	}

	if matcher.calls != 1 {
		t.Fatalf("MatchRegex() calls = %d, want 1", matcher.calls)
	}
}

func TestExpressionMatch_MatchesWithExpressionMatcher_ReturnsMatcherError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("compile failed")

	matcher := &fakeExpressionRegexMatcher{
		t:   t,
		err: wantErr,
	}

	match := ExpressionMatch{
		ExpressionRegex: ExpressionRegex{
			Expression: "^team-.*",
		},
	}

	got, err := match.MatchesWithExpressionMatcher(matcher, "team-a")
	if err == nil {
		t.Fatalf("MatchesWithExpressionMatcher() expected error, got nil")
	}

	if !errors.Is(err, wantErr) {
		t.Fatalf("MatchesWithExpressionMatcher() error = %v, want %v", err, wantErr)
	}

	if got {
		t.Fatalf("MatchesWithExpressionMatcher() = true, want false on error")
	}

	if matcher.calls != 1 {
		t.Fatalf("MatchRegex() calls = %d, want 1", matcher.calls)
	}
}

func TestExpressionMatch_MatchesAndMatchesWithExpressionMatcher_AgreeForNilMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		match ExpressionMatch
		value string
	}{
		{
			name: "exact only",
			match: ExpressionMatch{
				Exact: []string{"a", "b"},
			},
			value: "a",
		},
		{
			name: "regex only",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^a+$",
				},
			},
			value: "aaa",
		},
		{
			name: "combined exact and regex exact wins",
			match: ExpressionMatch{
				Exact: []string{"a"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^b+$",
				},
			},
			value: "a",
		},
		{
			name: "combined exact and regex regex wins",
			match: ExpressionMatch{
				Exact: []string{"a"},
				ExpressionRegex: ExpressionRegex{
					Expression: "^b+$",
				},
			},
			value: "bbb",
		},
		{
			name: "negated exact",
			match: ExpressionMatch{
				Exact: []string{"a"},
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			value: "b",
		},
		{
			name: "negated regex",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "^a+$",
					Negate:     true,
				},
			},
			value: "bbb",
		},
		{
			name: "invalid regex",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Expression: "[",
				},
			},
			value: "a",
		},
		{
			name:  "empty matcher",
			match: ExpressionMatch{},
			value: "a",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMatches, errMatches := tt.match.Matches(tt.value)
			gotWithMatcher, errWithMatcher := tt.match.MatchesWithExpressionMatcher(nil, tt.value)

			if (errMatches != nil) != (errWithMatcher != nil) {
				t.Fatalf(
					"error mismatch: Matches() err=%v, MatchesWithExpressionMatcher(nil) err=%v",
					errMatches,
					errWithMatcher,
				)
			}

			if gotMatches != gotWithMatcher {
				t.Fatalf(
					"result mismatch: Matches()=%t, MatchesWithExpressionMatcher(nil)=%t",
					gotMatches,
					gotWithMatcher,
				)
			}
		})
	}
}

func TestContainsExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		value  string
		want   bool
	}{
		{
			name:   "nil values",
			values: nil,
			value:  "a",
			want:   false,
		},
		{
			name:   "empty values",
			values: []string{},
			value:  "a",
			want:   false,
		},
		{
			name:   "contains value",
			values: []string{"a", "b", "c"},
			value:  "b",
			want:   true,
		},
		{
			name:   "does not contain value",
			values: []string{"a", "b", "c"},
			value:  "d",
			want:   false,
		},
		{
			name:   "case sensitive",
			values: []string{"A"},
			value:  "a",
			want:   false,
		},
		{
			name:   "empty string",
			values: []string{""},
			value:  "",
			want:   true,
		},
		{
			name:   "whitespace is significant",
			values: []string{" a "},
			value:  "a",
			want:   false,
		},
		{
			name:   "whitespace matches exactly",
			values: []string{" a "},
			value:  " a ",
			want:   true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := containsExact(tt.values, tt.value)
			if got != tt.want {
				t.Fatalf("containsExact(%v, %q) = %t, want %t", tt.values, tt.value, got, tt.want)
			}
		})
	}
}

func TestExpressionMatch_applyNegate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		match   ExpressionMatch
		matched bool
		want    bool
	}{
		{
			name:    "non negated true",
			match:   ExpressionMatch{},
			matched: true,
			want:    true,
		},
		{
			name:    "non negated false",
			match:   ExpressionMatch{},
			matched: false,
			want:    false,
		},
		{
			name: "negated true",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			matched: true,
			want:    false,
		},
		{
			name: "negated false",
			match: ExpressionMatch{
				ExpressionRegex: ExpressionRegex{
					Negate: true,
				},
			},
			matched: false,
			want:    true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.match.applyNegate(tt.matched)
			if got != tt.want {
				t.Fatalf("applyNegate(%t) = %t, want %t", tt.matched, got, tt.want)
			}
		})
	}
}
