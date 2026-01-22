// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package cache

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestNamespaceRegistriesCache_DedupAndRefCount(t *testing.T) {
	c := NewNamespaceRegistriesCache()

	rulesA := []api.OCIRegistry{
		{
			Registry:   "^docker\\.io/.*$",
			Policy:     []corev1.PullPolicy{corev1.PullIfNotPresent},
			Validation: []api.RegistryValidationTarget{"Images"},
		},
	}

	// Set ns1 -> A
	if err := c.Set("ns1", rulesA); err != nil {
		t.Fatalf("Set ns1: %v", err)
	}
	ns, uniq := c.Stats()
	if ns != 1 || uniq != 1 {
		t.Fatalf("Stats after ns1 set: namespaces=%d unique=%d, want 1/1", ns, uniq)
	}

	// Set ns2 -> A (should dedup, so unique stays 1)
	if err := c.Set("ns2", rulesA); err != nil {
		t.Fatalf("Set ns2: %v", err)
	}
	ns, uniq = c.Stats()
	if ns != 2 || uniq != 1 {
		t.Fatalf("Stats after ns2 set: namespaces=%d unique=%d, want 2/1", ns, uniq)
	}

	// Pointers should be identical (dedup reuse)
	rs1, ok := c.Get("ns1")
	if !ok {
		t.Fatalf("Get ns1: expected ok")
	}
	rs2, ok := c.Get("ns2")
	if !ok {
		t.Fatalf("Get ns2: expected ok")
	}
	if rs1 != rs2 {
		t.Fatalf("ruleset pointers differ; expected dedup reuse")
	}
	if !rs1.HasImages || rs1.HasVolumes {
		t.Fatalf("ruleset flags: HasImages=%v HasVolumes=%v, want true/false", rs1.HasImages, rs1.HasVolumes)
	}

	// Delete ns1: unique still 1 because ns2 references
	c.Delete("ns1")
	ns, uniq = c.Stats()
	if ns != 1 || uniq != 1 {
		t.Fatalf("Stats after delete ns1: namespaces=%d unique=%d, want 1/1", ns, uniq)
	}

	// Delete ns2: now unique becomes 0
	c.Delete("ns2")
	ns, uniq = c.Stats()
	if ns != 0 || uniq != 0 {
		t.Fatalf("Stats after delete ns2: namespaces=%d unique=%d, want 0/0", ns, uniq)
	}
}

func TestNamespaceRegistriesCache_SetSameRulesIsIdempotent(t *testing.T) {
	c := NewNamespaceRegistriesCache()

	rules := []api.OCIRegistry{
		{Registry: "^gcr\\.io/.*$"},
	}

	if err := c.Set("ns1", rules); err != nil {
		t.Fatalf("Set 1: %v", err)
	}
	rsBefore, ok := c.Get("ns1")
	if !ok {
		t.Fatalf("Get ns1: expected ok")
	}

	// Setting same rules again should be a no-op (same ID)
	if err := c.Set("ns1", rules); err != nil {
		t.Fatalf("Set 2: %v", err)
	}
	rsAfter, ok := c.Get("ns1")
	if !ok {
		t.Fatalf("Get ns1 after: expected ok")
	}
	if rsBefore != rsAfter {
		t.Fatalf("expected same pointer after idempotent Set")
	}

	ns, uniq := c.Stats()
	if ns != 1 || uniq != 1 {
		t.Fatalf("Stats: namespaces=%d unique=%d, want 1/1", ns, uniq)
	}
}

func TestNamespaceRegistriesCache_ReassignNamespaceAdjustsRefCounts(t *testing.T) {
	c := NewNamespaceRegistriesCache()

	rulesA := []api.OCIRegistry{
		{Registry: "^docker\\.io/.*$", Validation: []api.RegistryValidationTarget{"Images"}},
	}
	rulesB := []api.OCIRegistry{
		{Registry: "^ghcr\\.io/.*$", Validation: []api.RegistryValidationTarget{"Volumes"}},
	}

	// ns1 -> A, ns2 -> A
	if err := c.Set("ns1", rulesA); err != nil {
		t.Fatalf("Set ns1 A: %v", err)
	}
	if err := c.Set("ns2", rulesA); err != nil {
		t.Fatalf("Set ns2 A: %v", err)
	}

	// ns2 -> B (move one namespace)
	if err := c.Set("ns2", rulesB); err != nil {
		t.Fatalf("Set ns2 B: %v", err)
	}

	ns, uniq := c.Stats()
	if ns != 2 || uniq != 2 {
		t.Fatalf("Stats after move: namespaces=%d unique=%d, want 2/2", ns, uniq)
	}

	rs1, _ := c.Get("ns1")
	rs2, _ := c.Get("ns2")
	if rs1 == rs2 {
		t.Fatalf("expected different pointers for different rulesets")
	}
	if !rs1.HasImages || rs1.HasVolumes {
		t.Fatalf("ns1 flags: HasImages=%v HasVolumes=%v, want true/false", rs1.HasImages, rs1.HasVolumes)
	}
	if rs2.HasImages || !rs2.HasVolumes {
		t.Fatalf("ns2 flags: HasImages=%v HasVolumes=%v, want false/true", rs2.HasImages, rs2.HasVolumes)
	}

	// Now delete ns1: ruleset A should be removed (no more refs)
	c.Delete("ns1")
	ns, uniq = c.Stats()
	if ns != 1 || uniq != 1 {
		t.Fatalf("Stats after delete ns1: namespaces=%d unique=%d, want 1/1", ns, uniq)
	}

	// ns2 still exists
	if _, ok := c.Get("ns2"); !ok {
		t.Fatalf("expected ns2 still present")
	}
}

func TestNamespaceRegistriesCache_DeleteMissingIsNoop(t *testing.T) {
	c := NewNamespaceRegistriesCache()

	// Should not panic
	c.Delete("does-not-exist")

	ns, uniq := c.Stats()
	if ns != 0 || uniq != 0 {
		t.Fatalf("Stats: namespaces=%d unique=%d, want 0/0", ns, uniq)
	}
}

func TestNamespaceRegistriesCache_GetMissing(t *testing.T) {
	c := NewNamespaceRegistriesCache()

	if rs, ok := c.Get("missing"); ok || rs != nil {
		t.Fatalf("expected ok=false and rs=nil for missing namespace")
	}
}

func TestBuildRuleSet_InvalidRegexReturnsError(t *testing.T) {
	_, err := buildRuleSet([]api.OCIRegistry{
		{Registry: "(*invalid-regex"},
	})
	if err == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

func TestHashRules_PolicyAndValidationOrderDoesNotAffectHash(t *testing.T) {
	// Same rule, policy order flipped, validation order flipped => same ID
	r1 := []api.OCIRegistry{
		{
			Registry: "^example\\.com/.*$",
			Policy: []corev1.PullPolicy{
				corev1.PullAlways,
				corev1.PullIfNotPresent,
			},
			Validation: []api.RegistryValidationTarget{
				"Volumes",
				"Images",
			},
		},
	}
	r2 := []api.OCIRegistry{
		{
			Registry: "^example\\.com/.*$",
			Policy: []corev1.PullPolicy{
				corev1.PullIfNotPresent,
				corev1.PullAlways,
			},
			Validation: []api.RegistryValidationTarget{
				"Images",
				"Volumes",
			},
		},
	}

	id1 := hashRules(r1)
	id2 := hashRules(r2)
	if id1 != id2 {
		t.Fatalf("expected same hash, got %s vs %s", id1, id2)
	}
}

func TestHashRules_RuleOrderAffectsHash(t *testing.T) {
	// IMPORTANT: rule order is preserved and should change the hash
	aThenB := []api.OCIRegistry{
		{Registry: "^a\\.io/.*$", Policy: []corev1.PullPolicy{corev1.PullAlways}},
		{Registry: "^b\\.io/.*$", Policy: []corev1.PullPolicy{corev1.PullIfNotPresent}},
	}
	bThenA := []api.OCIRegistry{
		{Registry: "^b\\.io/.*$", Policy: []corev1.PullPolicy{corev1.PullIfNotPresent}},
		{Registry: "^a\\.io/.*$", Policy: []corev1.PullPolicy{corev1.PullAlways}},
	}

	id1 := hashRules(aThenB)
	id2 := hashRules(bThenA)
	if id1 == id2 {
		t.Fatalf("expected different hash when rule order differs")
	}
}

func TestBuildRuleSet_AllowedPolicyNilWhenEmpty(t *testing.T) {
	rs, err := buildRuleSet([]api.OCIRegistry{
		{Registry: "^example\\.com/.*$"},
	})
	if err != nil {
		t.Fatalf("buildRuleSet: %v", err)
	}
	if len(rs.Compiled) != 1 {
		t.Fatalf("expected 1 compiled rule, got %d", len(rs.Compiled))
	}
	if rs.Compiled[0].AllowedPolicy != nil {
		t.Fatalf("expected AllowedPolicy nil when no policies specified")
	}
}

func TestBuildRuleSet_FlagsAggregateAcrossRules(t *testing.T) {
	rs, err := buildRuleSet([]api.OCIRegistry{
		{Registry: "^img\\.io/.*$", Validation: []api.RegistryValidationTarget{"Images"}},
		{Registry: "^vol\\.io/.*$", Validation: []api.RegistryValidationTarget{"Volumes"}},
	})
	if err != nil {
		t.Fatalf("buildRuleSet: %v", err)
	}
	if !rs.HasImages || !rs.HasVolumes {
		t.Fatalf("expected HasImages and HasVolumes true, got %v/%v", rs.HasImages, rs.HasVolumes)
	}
	if len(rs.Compiled) != 2 {
		t.Fatalf("expected 2 compiled rules, got %d", len(rs.Compiled))
	}
	if !rs.Compiled[0].ValidateImages || rs.Compiled[0].ValidateVolumes {
		t.Fatalf("rule0 validations unexpected: images=%v volumes=%v",
			rs.Compiled[0].ValidateImages, rs.Compiled[0].ValidateVolumes)
	}
	if rs.Compiled[1].ValidateImages || !rs.Compiled[1].ValidateVolumes {
		t.Fatalf("rule1 validations unexpected: images=%v volumes=%v",
			rs.Compiled[1].ValidateImages, rs.Compiled[1].ValidateVolumes)
	}
}
