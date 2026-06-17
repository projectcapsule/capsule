// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

type RuleSet struct {
	ID       string
	Compiled []CompiledRule
}

type CompiledRule struct {
	Expression api.ExpressionRegex
	RegexID    string

	AllowedPolicy map[corev1.PullPolicy]struct{} // nil/empty => allow any
}

func (r *CompiledRule) AllowsPullPolicy(pullPolicy corev1.PullPolicy) bool {
	if len(r.AllowedPolicy) == 0 {
		return true
	}

	_, ok := r.AllowedPolicy[pullPolicy]

	return ok
}

type RegistryRuleSetCache struct {
	regexCache *RegexCache

	mu sync.RWMutex
	rs map[string]*RuleSet
}

func NewRegistryRuleSetCache(regexCache *RegexCache) *RegistryRuleSetCache {
	if regexCache == nil {
		regexCache = NewRegexCache()
	}

	return &RegistryRuleSetCache{
		regexCache: regexCache,
		rs:         make(map[string]*RuleSet),
	}
}

func (c *RegistryRuleSetCache) GetOrBuild(specRules []rules.OCIRegistry) (rs *RuleSet, fromCache bool, err error) {
	if len(specRules) == 0 {
		return nil, false, nil
	}

	if c == nil {
		return nil, false, fmt.Errorf("registry rule set cache is nil")
	}

	id := c.HashRules(specRules)

	c.mu.RLock()
	rs = c.rs[id]
	c.mu.RUnlock()

	if rs != nil {
		return rs, true, nil
	}

	built, err := c.buildRuleSet(id, specRules)
	if err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rs == nil {
		c.rs = make(map[string]*RuleSet)
	}

	if rs = c.rs[id]; rs != nil {
		return rs, true, nil
	}

	c.rs[id] = built

	return built, false, nil
}

// Match matches reference, regex and pullPolicy.
// Admission evaluation should usually use MatchReference instead.
func (c *RegistryRuleSetCache) Match(
	specRules []rules.OCIRegistry,
	reference string,
	pullPolicy corev1.PullPolicy,
) (*CompiledRule, error) {
	rs, _, err := c.GetOrBuild(specRules)
	if err != nil {
		return nil, err
	}

	if rs == nil {
		return nil, nil
	}

	return c.MatchRuleSet(rs, reference, pullPolicy)
}

func (c *RegistryRuleSetCache) MatchRuleSet(
	rs *RuleSet,
	reference string,
	pullPolicy corev1.PullPolicy,
) (*CompiledRule, error) {
	if c == nil {
		return nil, fmt.Errorf("registry rule set cache is nil")
	}

	if c.regexCache == nil {
		return nil, fmt.Errorf("regex cache is nil")
	}

	if rs == nil {
		return nil, nil
	}

	for i := range rs.Compiled {
		rule := &rs.Compiled[i]

		if !rule.AllowsPullPolicy(pullPolicy) {
			continue
		}

		compiled, _, err := c.regexCache.GetOrCompile(rule.Expression)
		if err != nil {
			return nil, err
		}

		if compiled.MatchString(reference) {
			return rule, nil
		}
	}

	return nil, nil
}

// MatchReference matches reference and regex only.
// It intentionally does not check pullPolicy.
func (c *RegistryRuleSetCache) MatchReference(
	rs *RuleSet,
	reference string,
) (*CompiledRule, error) {
	if c == nil {
		return nil, fmt.Errorf("registry rule set cache is nil")
	}

	if c.regexCache == nil {
		return nil, fmt.Errorf("regex cache is nil")
	}

	if rs == nil {
		return nil, nil
	}

	for i := range rs.Compiled {
		rule := &rs.Compiled[i]

		compiled, _, err := c.regexCache.GetOrCompile(rule.Expression)
		if err != nil {
			return nil, err
		}

		if compiled.MatchString(reference) {
			return rule, nil
		}
	}

	return nil, nil
}

func (c *RegistryRuleSetCache) Stats() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.rs)
}

func (c *RegistryRuleSetCache) PruneActive(activeIDs map[string]struct{}) int {
	if c == nil {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0

	for id := range c.rs {
		if _, ok := activeIDs[id]; ok {
			continue
		}

		delete(c.rs, id)

		removed++
	}

	return removed
}

func (c *RegistryRuleSetCache) HashRules(specRules []rules.OCIRegistry) string {
	var b strings.Builder

	b.Grow(len(specRules) * 96)

	const (
		sepRule  = "\n"
		sepField = "\x1f"
		sepList  = "\x1e"
	)

	for _, r := range specRules {
		expr := r.Expression()

		policies := make([]string, 0, len(r.Policy))
		for _, p := range r.Policy {
			policies = append(policies, strings.TrimSpace(string(p)))
		}

		sort.Strings(policies)

		b.WriteString(strings.TrimSpace(expr.Expression))
		b.WriteString(sepField)

		if expr.Negate {
			b.WriteString("1")
		} else {
			b.WriteString("0")
		}

		b.WriteString(sepField)

		for i, p := range policies {
			if i > 0 {
				b.WriteString(sepList)
			}

			b.WriteString(p)
		}

		b.WriteString(sepRule)
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}

func (c *RegistryRuleSetCache) Has(id string) bool {
	if c == nil {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.rs[id]

	return ok
}

func (c *RegistryRuleSetCache) Reset() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.rs = make(map[string]*RuleSet)
}

//nolint:unused
func (c *RegistryRuleSetCache) insertForTest(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rs == nil {
		c.rs = make(map[string]*RuleSet)
	}

	c.rs[id] = &RuleSet{ID: id}
}

func (c *RegistryRuleSetCache) buildRuleSet(id string, specRules []rules.OCIRegistry) (*RuleSet, error) {
	if c.regexCache == nil {
		return nil, fmt.Errorf("regex cache is nil")
	}

	rs := &RuleSet{
		ID:       id,
		Compiled: make([]CompiledRule, 0, len(specRules)),
	}

	for _, r := range specRules {
		expression := r.Expression()

		compiled, _, err := c.regexCache.GetOrCompile(expression)
		if err != nil {
			return nil, err
		}

		cr := CompiledRule{
			Expression: expression,
			RegexID:    compiled.ID,
		}

		if len(r.Policy) > 0 {
			cr.AllowedPolicy = make(map[corev1.PullPolicy]struct{}, len(r.Policy))

			for _, p := range r.Policy {
				cr.AllowedPolicy[p] = struct{}{}
			}
		}

		rs.Compiled = append(rs.Compiled, cr)
	}

	return rs, nil
}
