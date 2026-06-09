// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestRegistryRuleSetCache_GetOrBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		rules          []rules.OCIRegistry
		wantNil        bool
		wantErr        bool
		wantFromCache  bool
		wantRuleCount  int
		wantHasImages  bool
		wantHasVolumes bool
	}{
		{
			name:    "empty rules return nil",
			rules:   nil,
			wantNil: true,
		},
		{
			name: "build single rule",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
					Policy: []corev1.PullPolicy{
						corev1.PullIfNotPresent,
					},
					Validation: []rules.RegistryValidationTarget{
						rules.ValidateImages,
					},
				},
			},
			wantRuleCount:  1,
			wantHasImages:  true,
			wantHasVolumes: false,
		},
		{
			name: "empty validation defaults to images and volumes",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
				},
			},
			wantRuleCount:  1,
			wantHasImages:  true,
			wantHasVolumes: true,
		},
		{
			name: "invalid regex returns error",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `[`,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			regexCache := NewRegexCache()
			registryCache := NewRegistryRuleSetCache(regexCache)

			rs, fromCache, err := registryCache.GetOrBuild(tt.rules)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if rs != nil {
					t.Fatalf("expected nil ruleset on error, got %#v", rs)
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantNil {
				if rs != nil {
					t.Fatalf("expected nil ruleset, got %#v", rs)
				}

				return
			}

			if rs == nil {
				t.Fatal("expected ruleset, got nil")
			}

			if fromCache != tt.wantFromCache {
				t.Fatalf("expected fromCache=%t, got %t", tt.wantFromCache, fromCache)
			}

			if len(rs.Compiled) != tt.wantRuleCount {
				t.Fatalf("expected %d compiled rules, got %d", tt.wantRuleCount, len(rs.Compiled))
			}

			if rs.HasImages != tt.wantHasImages {
				t.Fatalf("expected HasImages=%t, got %t", tt.wantHasImages, rs.HasImages)
			}

			if rs.HasVolumes != tt.wantHasVolumes {
				t.Fatalf("expected HasVolumes=%t, got %t", tt.wantHasVolumes, rs.HasVolumes)
			}

			if got := registryCache.Stats(); got != 1 {
				t.Fatalf("expected 1 registry ruleset cache entry, got %d", got)
			}

			if got := regexCache.Stats(); got != tt.wantRuleCount {
				t.Fatalf("expected %d regex cache entries, got %d", tt.wantRuleCount, got)
			}
		})
	}
}

func TestRegistryRuleSetCache_GetOrBuild_ReusesCachedRuleSet(t *testing.T) {
	t.Parallel()

	regexCache := NewRegexCache()
	registryCache := NewRegistryRuleSetCache(regexCache)

	rules := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
		},
	}

	first, fromCache, err := registryCache.GetOrBuild(rules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if fromCache {
		t.Fatal("expected first lookup to build ruleset, got cache hit")
	}

	second, fromCache, err := registryCache.GetOrBuild(rules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !fromCache {
		t.Fatal("expected second lookup to hit cache")
	}

	if first != second {
		t.Fatal("expected cached ruleset pointer to be reused")
	}

	if got := registryCache.Stats(); got != 1 {
		t.Fatalf("expected 1 registry ruleset cache entry, got %d", got)
	}

	if got := regexCache.Stats(); got != 1 {
		t.Fatalf("expected 1 regex cache entry, got %d", got)
	}
}

func TestRegistryRuleSetCache_Match(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rules      []rules.OCIRegistry
		image      string
		pullPolicy corev1.PullPolicy
		target     rules.RegistryValidationTarget
		wantMatch  bool
		wantErr    bool
	}{
		{
			name: "match image with default validation and any pull policy",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  true,
		},
		{
			name: "match volume with default validation",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateVolumes,
			wantMatch:  true,
		},
		{
			name: "does not match wrong image",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
				},
			},
			image:      "docker.io/library/nginx:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  false,
		},
		{
			name: "does not match wrong pull policy",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
					Policy: []corev1.PullPolicy{
						corev1.PullAlways,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  false,
		},
		{
			name: "matches allowed pull policy",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
					Policy: []corev1.PullPolicy{
						corev1.PullIfNotPresent,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  true,
		},
		{
			name: "does not match wrong validation target",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
					Validation: []rules.RegistryValidationTarget{
						rules.ValidateVolumes,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  false,
		},
		{
			name: "matches configured validation target",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
					},
					Validation: []rules.RegistryValidationTarget{
						rules.ValidateImages,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  true,
		},
		{
			name: "negated regex matches non matching image",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
						Negate:     true,
					},
				},
			},
			image:      "docker.io/library/nginx:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  true,
		},
		{
			name: "negated regex does not match matching image",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `^ghcr\.io/projectcapsule/.*`,
						Negate:     true,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  false,
		},
		{
			name: "invalid regex returns error",
			rules: []rules.OCIRegistry{
				{
					RegExpression: api.RegExpression{
						Expression: `[`,
					},
				},
			},
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantErr:    true,
		},
		{
			name:       "empty rules do not match",
			rules:      nil,
			image:      "ghcr.io/projectcapsule/capsule:latest",
			pullPolicy: corev1.PullIfNotPresent,
			target:     rules.ValidateImages,
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registryCache := NewRegistryRuleSetCache(NewRegexCache())

			matched, err := registryCache.Match(
				tt.rules,
				tt.image,
				tt.pullPolicy,
				tt.target,
			)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if tt.wantMatch && matched == nil {
				t.Fatal("expected match, got nil")
			}

			if !tt.wantMatch && matched != nil {
				t.Fatalf("expected no match, got %#v", matched)
			}
		})
	}
}

func TestRegistryRuleSetCache_HashRules_NormalizesPolicyAndValidationOrder(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(NewRegexCache())

	a := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
			Policy: []corev1.PullPolicy{
				corev1.PullAlways,
				corev1.PullIfNotPresent,
			},
			Validation: []rules.RegistryValidationTarget{
				rules.ValidateImages,
				rules.ValidateVolumes,
			},
		},
	}

	b := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
			Policy: []corev1.PullPolicy{
				corev1.PullIfNotPresent,
				corev1.PullAlways,
			},
			Validation: []rules.RegistryValidationTarget{
				rules.ValidateVolumes,
				rules.ValidateImages,
			},
		},
	}

	if c.HashRules(a) != c.HashRules(b) {
		t.Fatal("expected equal hashes when policy and validation values only differ by order")
	}
}

func TestRegistryRuleSetCache_HashRules_UsesNegate(t *testing.T) {
	t.Parallel()

	c := NewRegistryRuleSetCache(NewRegexCache())

	positive := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/.*`,
			},
		},
	}

	negative := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/.*`,
				Negate:     true,
			},
		},
	}

	if c.HashRules(positive) == c.HashRules(negative) {
		t.Fatal("expected different hashes for negated and non-negated registry expressions")
	}
}

func TestRegistryRuleSetCache_PruneActive(t *testing.T) {
	t.Parallel()

	registryCache := NewRegistryRuleSetCache(NewRegexCache())

	keepRules := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
		},
	}

	removeRules := []rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^docker\.io/library/.*`,
			},
		},
	}

	keep, _, err := registryCache.GetOrBuild(keepRules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	remove, _, err := registryCache.GetOrBuild(removeRules)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := registryCache.Stats(); got != 2 {
		t.Fatalf("expected 2 cache entries before prune, got %d", got)
	}

	removed := registryCache.PruneActive(map[string]struct{}{
		keep.ID: {},
	})

	if removed != 1 {
		t.Fatalf("expected 1 pruned cache entry, got %d", removed)
	}

	if !registryCache.Has(keep.ID) {
		t.Fatalf("expected kept ruleset id %q to remain", keep.ID)
	}

	if registryCache.Has(remove.ID) {
		t.Fatalf("expected removed ruleset id %q to be pruned", remove.ID)
	}

	if got := registryCache.Stats(); got != 1 {
		t.Fatalf("expected 1 cache entry after prune, got %d", got)
	}
}

func TestRegistryRuleSetCache_Reset(t *testing.T) {
	t.Parallel()

	registryCache := NewRegistryRuleSetCache(NewRegexCache())

	rs, _, err := registryCache.GetOrBuild([]rules.OCIRegistry{
		{
			RegExpression: api.RegExpression{
				Expression: `^ghcr\.io/projectcapsule/.*`,
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := registryCache.Stats(); got != 1 {
		t.Fatalf("expected 1 cache entry, got %d", got)
	}

	registryCache.Reset()

	if got := registryCache.Stats(); got != 0 {
		t.Fatalf("expected 0 cache entries after reset, got %d", got)
	}

	if registryCache.Has(rs.ID) {
		t.Fatalf("expected ruleset id %q to be removed after reset", rs.ID)
	}
}

func TestCompiledRule_AllowsPullPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rule       CompiledRule
		pullPolicy corev1.PullPolicy
		want       bool
	}{
		{
			name:       "empty policy allows any",
			rule:       CompiledRule{},
			pullPolicy: corev1.PullAlways,
			want:       true,
		},
		{
			name: "configured policy allows matching value",
			rule: CompiledRule{
				AllowedPolicy: map[corev1.PullPolicy]struct{}{
					corev1.PullIfNotPresent: {},
				},
			},
			pullPolicy: corev1.PullIfNotPresent,
			want:       true,
		},
		{
			name: "configured policy rejects non matching value",
			rule: CompiledRule{
				AllowedPolicy: map[corev1.PullPolicy]struct{}{
					corev1.PullIfNotPresent: {},
				},
			},
			pullPolicy: corev1.PullAlways,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.rule.AllowsPullPolicy(tt.pullPolicy); got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestCompiledRule_MatchesTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rule   CompiledRule
		target rules.RegistryValidationTarget
		want   bool
	}{
		{
			name: "matches images",
			rule: CompiledRule{
				ValidateImages: true,
			},
			target: rules.ValidateImages,
			want:   true,
		},
		{
			name: "does not match images when only volumes configured",
			rule: CompiledRule{
				ValidateVolumes: true,
			},
			target: rules.ValidateImages,
			want:   false,
		},
		{
			name: "matches volumes",
			rule: CompiledRule{
				ValidateVolumes: true,
			},
			target: rules.ValidateVolumes,
			want:   true,
		},
		{
			name: "does not match unknown target",
			rule: CompiledRule{
				ValidateImages:  true,
				ValidateVolumes: true,
			},
			target: rules.RegistryValidationTarget("unknown"),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.rule.MatchesTarget(tt.target); got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}
