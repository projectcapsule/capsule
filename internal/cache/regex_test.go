// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestCompiledRegexMatchString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression api.RegExpression
		value      string
		want       bool
	}{
		{
			name: "normal expression matches matching value",
			expression: api.RegExpression{
				Expression: "trusted/.*",
			},
			value: "trusted/team/app:1",
			want:  true,
		},
		{
			name: "normal expression does not match non matching value",
			expression: api.RegExpression{
				Expression: "trusted/.*",
			},
			value: "docker.io/team/app:1",
			want:  false,
		},
		{
			name: "negated expression does not match matching value",
			expression: api.RegExpression{
				Expression: "trusted/.*",
				Negate:     true,
			},
			value: "trusted/team/app:1",
			want:  false,
		},
		{
			name: "negated expression matches non matching value",
			expression: api.RegExpression{
				Expression: "trusted/.*",
				Negate:     true,
			},
			value: "docker.io/team/app:1",
			want:  true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache := NewRegexCache()

			compiled, _, err := cache.GetOrCompile(tt.expression)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got := compiled.MatchString(tt.value); got != tt.want {
				t.Fatalf("MatchString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegexCache_GetOrCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		expression  api.RegExpression
		value       string
		wantMatch   bool
		wantErr     bool
		wantCached  bool
		wantEntries int
	}{
		{
			name: "compile matching regex",
			expression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
			value:       "ghcr.io/projectcapsule/capsule:latest",
			wantMatch:   true,
			wantErr:     false,
			wantCached:  false,
			wantEntries: 1,
		},
		{
			name: "compile non matching regex",
			expression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
			value:       "docker.io/library/nginx:latest",
			wantMatch:   false,
			wantErr:     false,
			wantCached:  false,
			wantEntries: 1,
		},
		{
			name: "compile negated matching regex",
			expression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
				Negate:     true,
			},
			value:       "ghcr.io/projectcapsule/capsule:latest",
			wantMatch:   false,
			wantErr:     false,
			wantCached:  false,
			wantEntries: 1,
		},
		{
			name: "compile negated non matching regex",
			expression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
				Negate:     true,
			},
			value:       "docker.io/library/nginx:latest",
			wantMatch:   true,
			wantErr:     false,
			wantCached:  false,
			wantEntries: 1,
		},
		{
			name: "reject empty expression",
			expression: api.RegExpression{
				Expression: "",
			},
			value:       "ghcr.io/projectcapsule/capsule:latest",
			wantErr:     true,
			wantEntries: 0,
		},
		{
			name: "reject invalid regex",
			expression: api.RegExpression{
				Expression: `[`,
			},
			value:       "ghcr.io/projectcapsule/capsule:latest",
			wantErr:     true,
			wantEntries: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewRegexCache()

			compiled, fromCache, err := c.GetOrCompile(tt.expression)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if compiled != nil {
					t.Fatalf("expected nil compiled regex on error, got %#v", compiled)
				}

				if got := c.Stats(); got != tt.wantEntries {
					t.Fatalf("expected %d cache entries, got %d", tt.wantEntries, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if compiled == nil {
				t.Fatal("expected compiled regex, got nil")
			}

			if fromCache != tt.wantCached {
				t.Fatalf("expected fromCache=%t, got %t", tt.wantCached, fromCache)
			}

			if got := compiled.MatchString(tt.value); got != tt.wantMatch {
				t.Fatalf("expected match=%t, got %t", tt.wantMatch, got)
			}

			if got := c.Stats(); got != tt.wantEntries {
				t.Fatalf("expected %d cache entries, got %d", tt.wantEntries, got)
			}

			if !c.Has(compiled.ID) {
				t.Fatalf("expected cache to contain regex id %q", compiled.ID)
			}
		})
	}
}

func TestRegexCache_GetOrCompile_ReusesCachedRegex(t *testing.T) {
	t.Parallel()

	c := NewRegexCache()

	expr := api.RegExpression{
		Expression: `^ghcr\.io/projectcapsule/.*`,
	}

	first, fromCache, err := c.GetOrCompile(expr)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if fromCache {
		t.Fatal("expected first lookup to build regex, got cache hit")
	}

	second, fromCache, err := c.GetOrCompile(expr)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !fromCache {
		t.Fatal("expected second lookup to hit cache")
	}

	if first != second {
		t.Fatal("expected cached regex pointer to be reused")
	}

	if got := c.Stats(); got != 1 {
		t.Fatalf("expected 1 cache entry, got %d", got)
	}
}

func TestRegexCache_HashRegex_UsesNegate(t *testing.T) {
	t.Parallel()

	positive := HashRegex(api.RegExpression{
		Expression: `^ghcr\.io/.*`,
	})

	negative := HashRegex(api.RegExpression{
		Expression: `^ghcr\.io/.*`,
		Negate:     true,
	})

	if positive == negative {
		t.Fatal("expected different hashes for negated and non-negated expressions")
	}
}

func TestRegexCache_Reset(t *testing.T) {
	t.Parallel()

	c := NewRegexCache()

	compiled, _, err := c.GetOrCompile(api.RegExpression{
		Expression: `^ghcr\.io/.*`,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := c.Stats(); got != 1 {
		t.Fatalf("expected 1 cache entry, got %d", got)
	}

	c.Reset()

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected 0 cache entries after reset, got %d", got)
	}

	if c.Has(compiled.ID) {
		t.Fatalf("expected regex id %q to be removed after reset", compiled.ID)
	}
}

func TestCompiledRegex_MatchString_NilSafe(t *testing.T) {
	t.Parallel()

	var compiled *CompiledRegex

	if compiled.MatchString("ghcr.io/projectcapsule/capsule:latest") {
		t.Fatal("expected nil compiled regex to return false")
	}

	compiled = &CompiledRegex{}

	if compiled.MatchString("ghcr.io/projectcapsule/capsule:latest") {
		t.Fatal("expected compiled regex with nil RE to return false")
	}
}
