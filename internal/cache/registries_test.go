// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestNewRegistryRuleSetCache(t *testing.T) {
	t.Parallel()

	t.Run("creates cache with default regex cache", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		if c == nil {
			t.Fatal("expected cache, got nil")
		}

		if c.regexCache == nil {
			t.Fatal("expected regex cache to be initialized")
		}

		if c.rs == nil {
			t.Fatal("expected rule set map to be initialized")
		}

		if got := c.Stats(); got != 0 {
			t.Fatalf("expected empty cache, got stats=%d", got)
		}
	})

	t.Run("uses provided regex cache", func(t *testing.T) {
		t.Parallel()

		regexCache := NewRegexCache()
		c := NewRegistryRuleSetCache(regexCache)

		if c.regexCache != regexCache {
			t.Fatal("expected provided regex cache to be used")
		}
	})
}

func TestRegistryRuleSetCacheGetOrBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rules     []rules.OCIRegistry
		wantNil   bool
		wantCache int
	}{
		{
			name:      "empty rules return nil ruleset",
			rules:     nil,
			wantNil:   true,
			wantCache: 0,
		},
		{
			name: "single registry builds ruleset",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
			},
			wantNil:   false,
			wantCache: 1,
		},
		{
			name: "multiple registries build one ruleset",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
				registry("ghcr.io/.*"),
			},
			wantNil:   false,
			wantCache: 1,
		},
		{
			name: "registry with policy builds ruleset",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
			},
			wantNil:   false,
			wantCache: 1,
		},
		{
			name: "registry with negated expression builds ruleset",
			rules: []rules.OCIRegistry{
				registryWithExpression(runtime.ExpressionRegex{
					Expression: "trusted/.*",
					Negate:     true,
				}),
			},
			wantNil:   false,
			wantCache: 1,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewRegistryRuleSetCache(nil)

			rs, fromCache, err := c.GetOrBuild(tt.rules)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if fromCache {
				t.Fatal("expected first build to not come from cache")
			}

			if tt.wantNil {
				if rs != nil {
					t.Fatalf("expected nil ruleset, got %#v", rs)
				}
			} else if rs == nil {
				t.Fatal("expected ruleset, got nil")
			}

			if got := c.Stats(); got != tt.wantCache {
				t.Fatalf("expected cache stats=%d, got %d", tt.wantCache, got)
			}
		})
	}
}

func TestRegistryRuleSetCacheGetOrBuildReturnsFromCache(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)
	specRules := []rules.OCIRegistry{
		registry("harbor/.*"),
		registryWithPolicy("ghcr.io/.*", corev1.PullIfNotPresent),
	}

	first, fromCache, err := c.GetOrBuild(specRules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if fromCache {
		t.Fatal("expected first call to build ruleset")
	}

	second, fromCache, err := c.GetOrBuild(specRules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !fromCache {
		t.Fatal("expected second call to come from cache")
	}

	if first != second {
		t.Fatal("expected same ruleset pointer from cache")
	}

	if got := c.Stats(); got != 1 {
		t.Fatalf("expected one cached ruleset, got %d", got)
	}
}

func TestRegistryRuleSetCacheGetOrBuildConcurrent(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)
	specRules := []rules.OCIRegistry{
		registry("harbor/.*"),
		registryWithPolicy("ghcr.io/.*", corev1.PullIfNotPresent),
	}

	const workers = 32

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	results := make(chan *RuleSet, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			rs, _, err := c.GetOrBuild(specRules)
			if err != nil {
				errs <- err

				return
			}

			results <- rs
		}()
	}

	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}

	var first *RuleSet
	for rs := range results {
		if rs == nil {
			t.Fatal("expected ruleset, got nil")
		}

		if first == nil {
			first = rs

			continue
		}

		if rs != first {
			t.Fatal("expected all goroutines to receive the same cached ruleset")
		}
	}

	if got := c.Stats(); got != 1 {
		t.Fatalf("expected one cached ruleset, got %d", got)
	}
}

func TestRegistryRuleSetCacheBuildRuleSet(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	specRules := []rules.OCIRegistry{
		registry("harbor/.*"),
		registryWithPolicy("ghcr.io/.*", corev1.PullAlways, corev1.PullIfNotPresent),
		registryWithExpression(runtime.ExpressionRegex{
			Expression: "trusted/.*",
			Negate:     true,
		}),
	}

	id := c.HashRules(specRules)

	rs, err := c.buildRuleSet(id, specRules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if rs == nil {
		t.Fatal("expected ruleset, got nil")
	}

	if rs.ID != id {
		t.Fatalf("expected ruleset ID %q, got %q", id, rs.ID)
	}

	if len(rs.Compiled) != len(specRules) {
		t.Fatalf("expected %d compiled rules, got %d", len(specRules), len(rs.Compiled))
	}

	if rs.Compiled[0].Match.Expression != "harbor/.*" {
		t.Fatalf("expected first expression harbor/.*, got %q", rs.Compiled[0].Match.Expression)
	}

	if len(rs.Compiled[0].AllowedPolicy) != 0 {
		t.Fatal("expected first rule to allow any pull policy")
	}

	if rs.Compiled[1].Match.Expression != "ghcr.io/.*" {
		t.Fatalf("expected second expression ghcr.io/.*, got %q", rs.Compiled[1].Match.Expression)
	}

	if len(rs.Compiled[1].AllowedPolicy) != 2 {
		t.Fatalf("expected two allowed pull policies, got %d", len(rs.Compiled[1].AllowedPolicy))
	}

	if _, ok := rs.Compiled[1].AllowedPolicy[corev1.PullAlways]; !ok {
		t.Fatal("expected PullAlways to be allowed")
	}

	if _, ok := rs.Compiled[1].AllowedPolicy[corev1.PullIfNotPresent]; !ok {
		t.Fatal("expected PullIfNotPresent to be allowed")
	}

	if !rs.Compiled[2].Match.Negate {
		t.Fatal("expected third expression to be negated")
	}
}

func TestRegistryRuleSetCacheBuildRuleSetInvalidRegex(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	_, _, err := c.GetOrBuild([]rules.OCIRegistry{
		registry("["),
	})
	if err == nil {
		t.Fatal("expected invalid regex error, got nil")
	}

	if !strings.Contains(err.Error(), "error parsing regexp") {
		t.Fatalf("expected regexp parse error, got %v", err)
	}
}

func TestRegistryRuleSetCacheMatchReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rules     []rules.OCIRegistry
		reference string
		wantMatch bool
		wantExpr  string
	}{
		{
			name: "matches first regex",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
			},
			reference: "harbor/team/app:1",
			wantMatch: true,
			wantExpr:  "harbor/.*",
		},
		{
			name: "does not match unmatched regex",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
			},
			reference: "ghcr.io/team/app:1",
			wantMatch: false,
		},
		{
			name: "returns first matching compiled rule",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
				registry("harbor/customer/.*"),
			},
			reference: "harbor/customer/app:1",
			wantMatch: true,
			wantExpr:  "harbor/.*",
		},
		{
			name: "matches later regex when first does not match",
			rules: []rules.OCIRegistry{
				registry("ghcr.io/.*"),
				registry("harbor/customer/.*"),
			},
			reference: "harbor/customer/app:1",
			wantMatch: true,
			wantExpr:  "harbor/customer/.*",
		},
		{
			name: "negated expression matches non-matching reference",
			rules: []rules.OCIRegistry{
				registryWithExpression(runtime.ExpressionRegex{
					Expression: "trusted/.*",
					Negate:     true,
				}),
			},
			// regexp.MatchString is not anchored by default. Do not use
			// "untrusted/..." here, because it contains "trusted/..." as a substring.
			reference: "docker.io/team/app:1",
			wantMatch: true,
			wantExpr:  "trusted/.*",
		},
		{
			name: "negated expression does not match matching reference",
			rules: []rules.OCIRegistry{
				registryWithExpression(runtime.ExpressionRegex{
					Expression: "trusted/.*",
					Negate:     true,
				}),
			},
			reference: "trusted/team/app:1",
			wantMatch: false,
		},
		{
			name: "policy does not affect MatchReference",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
			},
			reference: "harbor/team/app:1",
			wantMatch: true,
			wantExpr:  "harbor/.*",
		},
		{
			name: "legacy url fallback is used as expression",
			rules: []rules.OCIRegistry{
				registry("legacy/.*"),
			},
			reference: "legacy/team/app:1",
			wantMatch: true,
			wantExpr:  "legacy/.*",
		},
		{
			name: "nested regex expression wins over legacy url",
			rules: []rules.OCIRegistry{
				registryWithExpression(runtime.ExpressionRegex{
					Expression: "nested/.*",
				}),
			},
			reference: "nested/team/app:1",
			wantMatch: true,
			wantExpr:  "nested/.*",
		},
		{
			name: "legacy url is ignored when nested regex expression is set",
			rules: []rules.OCIRegistry{
				registryWithExpression(runtime.ExpressionRegex{
					Expression: "nested/.*",
				}),
			},
			reference: "legacy/team/app:1",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewRegistryRuleSetCache(nil)

			rs, _, err := c.GetOrBuild(tt.rules)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got, err := c.MatchReference(rs, tt.reference)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantMatch {
				if got == nil {
					t.Fatal("expected match, got nil")
				}

				if got.Match.Expression != tt.wantExpr {
					t.Fatalf("expected expression %q, got %q", tt.wantExpr, got.Match.Expression)
				}

				return
			}

			if got != nil {
				t.Fatalf("expected no match, got %#v", got)
			}
		})
	}
}

func TestRegistryRuleSetCacheMatchRuleSetWithPullPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rules      []rules.OCIRegistry
		reference  string
		pullPolicy corev1.PullPolicy
		wantMatch  bool
		wantExpr   string
	}{
		{
			name: "matches when no policy is configured",
			rules: []rules.OCIRegistry{
				registry("harbor/.*"),
			},
			reference:  "harbor/team/app:1",
			pullPolicy: corev1.PullAlways,
			wantMatch:  true,
			wantExpr:   "harbor/.*",
		},
		{
			name: "matches allowed pull policy",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
			},
			reference:  "harbor/team/app:1",
			pullPolicy: corev1.PullNever,
			wantMatch:  true,
			wantExpr:   "harbor/.*",
		},
		{
			name: "does not match forbidden pull policy",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
			},
			reference:  "harbor/team/app:1",
			pullPolicy: corev1.PullIfNotPresent,
			wantMatch:  false,
		},
		{
			name: "does not match empty pull policy when policy is configured",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
			},
			reference:  "harbor/team/app:1",
			pullPolicy: "",
			wantMatch:  false,
		},
		{
			name: "later rule can match when earlier policy rejects",
			rules: []rules.OCIRegistry{
				registryWithPolicy("harbor/.*", corev1.PullNever),
				registryWithPolicy("harbor/customer/.*", corev1.PullIfNotPresent),
			},
			reference:  "harbor/customer/app:1",
			pullPolicy: corev1.PullIfNotPresent,
			wantMatch:  true,
			wantExpr:   "harbor/customer/.*",
		},
		{
			name: "negated expression respects pull policy",
			rules: []rules.OCIRegistry{
				registryWithExpressionAndPolicy(runtime.ExpressionRegex{
					Expression: "trusted/.*",
					Negate:     true,
				}, corev1.PullIfNotPresent),
			},
			// regexp.MatchString is not anchored by default. Do not use
			// "untrusted/..." here, because it contains "trusted/..." as a substring.
			reference:  "docker.io/team/app:1",
			pullPolicy: corev1.PullIfNotPresent,
			wantMatch:  true,
			wantExpr:   "trusted/.*",
		},
		{
			name: "negated expression still rejects forbidden pull policy",
			rules: []rules.OCIRegistry{
				registryWithExpressionAndPolicy(runtime.ExpressionRegex{
					Expression: "trusted/.*",
					Negate:     true,
				}, corev1.PullNever),
			},
			// regexp.MatchString is not anchored by default. Do not use
			// "untrusted/..." here, because it contains "trusted/..." as a substring.
			reference:  "docker.io/team/app:1",
			pullPolicy: corev1.PullIfNotPresent,
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewRegistryRuleSetCache(nil)

			rs, _, err := c.GetOrBuild(tt.rules)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			got, err := c.MatchRuleSet(rs, tt.reference, tt.pullPolicy)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantMatch {
				if got == nil {
					t.Fatal("expected match, got nil")
				}

				if got.Match.Expression != tt.wantExpr {
					t.Fatalf("expected expression %q, got %q", tt.wantExpr, got.Match.Expression)
				}

				return
			}

			if got != nil {
				t.Fatalf("expected no match, got %#v", got)
			}
		})
	}
}

func TestRegistryRuleSetCacheMatch(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	got, err := c.Match(
		[]rules.OCIRegistry{
			registryWithPolicy("harbor/.*", corev1.PullIfNotPresent),
		},
		"harbor/team/app:1",
		corev1.PullIfNotPresent,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got == nil {
		t.Fatal("expected match, got nil")
	}

	if got.Match.Expression != "harbor/.*" {
		t.Fatalf("expected harbor/.*, got %q", got.Match.Expression)
	}
}

func TestCompiledRuleAllowsPullPolicy(t *testing.T) {
	t.Parallel()

	t.Run("nil policy allows any pull policy", func(t *testing.T) {
		t.Parallel()

		rule := &CompiledRule{}

		if !rule.AllowsPullPolicy(corev1.PullAlways) {
			t.Fatal("expected PullAlways to be allowed")
		}

		if !rule.AllowsPullPolicy(corev1.PullIfNotPresent) {
			t.Fatal("expected PullIfNotPresent to be allowed")
		}

		if !rule.AllowsPullPolicy(corev1.PullNever) {
			t.Fatal("expected PullNever to be allowed")
		}

		if !rule.AllowsPullPolicy("") {
			t.Fatal("expected empty pull policy to be allowed when no policy is configured")
		}
	})

	t.Run("configured policy allows only configured values", func(t *testing.T) {
		t.Parallel()

		rule := &CompiledRule{
			AllowedPolicy: map[corev1.PullPolicy]struct{}{
				corev1.PullNever: {},
			},
		}

		if !rule.AllowsPullPolicy(corev1.PullNever) {
			t.Fatal("expected PullNever to be allowed")
		}

		if rule.AllowsPullPolicy(corev1.PullIfNotPresent) {
			t.Fatal("expected PullIfNotPresent to be rejected")
		}

		if rule.AllowsPullPolicy("") {
			t.Fatal("expected empty pull policy to be rejected")
		}
	})
}

func TestRegistryRuleSetCacheHashRules(t *testing.T) {
	t.Parallel()

	t.Run("same rules produce same hash", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		a := []rules.OCIRegistry{
			registryWithPolicy("harbor/.*", corev1.PullNever, corev1.PullIfNotPresent),
			registry("ghcr.io/.*"),
		}

		b := []rules.OCIRegistry{
			registryWithPolicy("harbor/.*", corev1.PullIfNotPresent, corev1.PullNever),
			registry("ghcr.io/.*"),
		}

		hashA := c.HashRules(a)
		hashB := c.HashRules(b)

		if hashA != hashB {
			t.Fatalf("expected equal hash, got %q and %q", hashA, hashB)
		}
	})

	t.Run("different expression produces different hash", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		hashA := c.HashRules([]rules.OCIRegistry{
			registry("harbor/.*"),
		})

		hashB := c.HashRules([]rules.OCIRegistry{
			registry("ghcr.io/.*"),
		})

		if hashA == hashB {
			t.Fatalf("expected different hashes, got %q", hashA)
		}
	})

	t.Run("different negate value produces different hash", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		hashA := c.HashRules([]rules.OCIRegistry{
			registryWithExpression(runtime.ExpressionRegex{
				Expression: "trusted/.*",
				Negate:     false,
			}),
		})

		hashB := c.HashRules([]rules.OCIRegistry{
			registryWithExpression(runtime.ExpressionRegex{
				Expression: "trusted/.*",
				Negate:     true,
			}),
		})

		if hashA == hashB {
			t.Fatalf("expected different hashes, got %q", hashA)
		}
	})

	t.Run("different policy produces different hash", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		hashA := c.HashRules([]rules.OCIRegistry{
			registryWithPolicy("harbor/.*", corev1.PullNever),
		})

		hashB := c.HashRules([]rules.OCIRegistry{
			registryWithPolicy("harbor/.*", corev1.PullIfNotPresent),
		})

		if hashA == hashB {
			t.Fatalf("expected different hashes, got %q", hashA)
		}
	})

	t.Run("rule order affects hash", func(t *testing.T) {
		t.Parallel()

		c := NewRegistryRuleSetCache(nil)

		hashA := c.HashRules([]rules.OCIRegistry{
			registry("harbor/.*"),
			registry("ghcr.io/.*"),
		})

		hashB := c.HashRules([]rules.OCIRegistry{
			registry("ghcr.io/.*"),
			registry("harbor/.*"),
		})

		if hashA == hashB {
			t.Fatalf("expected different hashes because rule order matters, got %q", hashA)
		}
	})
}

func TestRegistryRuleSetCacheHasResetPruneActive(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	specA := []rules.OCIRegistry{
		registry("harbor/.*"),
	}

	specB := []rules.OCIRegistry{
		registry("ghcr.io/.*"),
	}

	idA := c.HashRules(specA)
	idB := c.HashRules(specB)

	if c.Has(idA) {
		t.Fatal("expected cache not to have idA before build")
	}

	if _, _, err := c.GetOrBuild(specA); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if _, _, err := c.GetOrBuild(specB); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !c.Has(idA) {
		t.Fatal("expected cache to have idA")
	}

	if !c.Has(idB) {
		t.Fatal("expected cache to have idB")
	}

	if got := c.Stats(); got != 2 {
		t.Fatalf("expected two cached rulesets, got %d", got)
	}

	removed := c.PruneActive(map[string]struct{}{
		idA: {},
	})

	if removed != 1 {
		t.Fatalf("expected one removed ruleset, got %d", removed)
	}

	if !c.Has(idA) {
		t.Fatal("expected cache to retain idA")
	}

	if c.Has(idB) {
		t.Fatal("expected cache to prune idB")
	}

	c.Reset()

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected cache to be empty after reset, got %d", got)
	}
}

func TestRegistryRuleSetCacheNilReceivers(t *testing.T) {
	t.Parallel()

	var c *RegistryRuleSetCache

	if got := c.Stats(); got != 0 {
		t.Fatalf("expected nil cache stats to be 0, got %d", got)
	}

	if c.Has("missing") {
		t.Fatal("expected nil cache Has to be false")
	}

	if removed := c.PruneActive(nil); removed != 0 {
		t.Fatalf("expected nil cache prune to remove 0, got %d", removed)
	}

	c.Reset()

	if _, _, err := c.GetOrBuild([]rules.OCIRegistry{registry("harbor/.*")}); err == nil {
		t.Fatal("expected nil cache GetOrBuild error")
	}

	if _, err := c.MatchRuleSet(&RuleSet{}, "harbor/app:1", corev1.PullAlways); err == nil {
		t.Fatal("expected nil cache MatchRuleSet error")
	}

	if _, err := c.MatchReference(&RuleSet{}, "harbor/app:1"); err == nil {
		t.Fatal("expected nil cache MatchReference error")
	}
}

func TestRegistryRuleSetCacheNilRegexCache(t *testing.T) {
	t.Parallel()

	c := &RegistryRuleSetCache{
		rs: make(map[string]*RuleSet),
	}

	if _, err := c.buildRuleSet("id", []rules.OCIRegistry{registry("harbor/.*")}); err == nil {
		t.Fatal("expected nil regex cache build error")
	}

	if _, err := c.MatchRuleSet(&RuleSet{}, "harbor/app:1", corev1.PullAlways); err == nil {
		t.Fatal("expected nil regex cache MatchRuleSet error")
	}

	if _, err := c.MatchReference(&RuleSet{}, "harbor/app:1"); err == nil {
		t.Fatal("expected nil regex cache MatchReference error")
	}
}

func TestRegistryRuleSetCacheMatchNilRuleSet(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	got, err := c.MatchRuleSet(nil, "harbor/app:1", corev1.PullAlways)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != nil {
		t.Fatalf("expected nil match, got %#v", got)
	}

	got, err = c.MatchReference(nil, "harbor/app:1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != nil {
		t.Fatalf("expected nil match, got %#v", got)
	}
}

func TestRegistryRuleSetCacheInsertForTest(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(nil)

	c.insertForTest("test-id")

	if !c.Has("test-id") {
		t.Fatal("expected inserted test id to exist")
	}
}

func registry(expression string) rules.OCIRegistry {
	return rules.OCIRegistry{
		ExpressionMatch: runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
				Negate:     false,
			},
		},
	}
}

func registryWithPolicy(expression string, policies ...corev1.PullPolicy) rules.OCIRegistry {
	return rules.OCIRegistry{
		ExpressionMatch: runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
				Negate:     false,
			},
		},
		Policy: policies,
	}
}

func registryWithExpression(expression runtime.ExpressionRegex) rules.OCIRegistry {
	return rules.OCIRegistry{
		ExpressionMatch: runtime.ExpressionMatch{
			ExpressionRegex: expression,
		},
	}
}

func registryWithExpressionAndPolicy(
	expression runtime.ExpressionRegex,
	policies ...corev1.PullPolicy,
) rules.OCIRegistry {
	return rules.OCIRegistry{
		ExpressionMatch: runtime.ExpressionMatch{
			ExpressionRegex: expression,
		},
		Policy: policies,
	}
}
