// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

type RuleSet struct {
	ID         string
	Compiled   []CompiledRule
	HasImages  bool
	HasVolumes bool
}

type CompiledRule struct {
	Registry        string
	RE              *regexp.Regexp
	AllowedPolicy   map[corev1.PullPolicy]struct{} // nil/empty => allow any
	ValidateImages  bool
	ValidateVolumes bool
}

type RegistryRuleSetCache struct {
	mu sync.RWMutex
	rs map[string]*RuleSet
}

func NewRegistryRuleSetCache() *RegistryRuleSetCache {
	return &RegistryRuleSetCache{
		rs: make(map[string]*RuleSet),
	}
}

func (c *RegistryRuleSetCache) GetOrBuild(specRules []api.OCIRegistry) (rs *RuleSet, fromCache bool, err error) {
	if len(specRules) == 0 {
		return nil, false, nil
	}

	id := c.HashRules(specRules)

	c.mu.RLock()
	rs = c.rs[id]
	c.mu.RUnlock()

	if rs != nil {
		return rs, true, nil
	}

	// Build outside locks (regex compile etc.)
	built, err := buildRuleSet(id, specRules)
	if err != nil {
		return nil, false, err
	}

	// Insert with double-check
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rs == nil {
		c.rs = make(map[string]*RuleSet)
	}

	// Another goroutine may have inserted meanwhile
	if rs = c.rs[id]; rs != nil {
		return rs, true, nil
	}

	c.rs[id] = built

	return built, false, nil
}

func (c *RegistryRuleSetCache) Stats() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.rs)
}

// activeIDs: set of ids currently referenced by RuleStatus in cluster.
func (c *RegistryRuleSetCache) PruneActive(activeIDs map[string]struct{}) int {
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

func (c *RegistryRuleSetCache) HashRules(specRules []api.OCIRegistry) string {
	var b strings.Builder

	b.Grow(len(specRules) * 64)

	const (
		sepRule  = "\n"
		sepField = "\x1f"
		sepList  = "\x1e"
	)

	for _, r := range specRules {
		url := strings.TrimSpace(r.Registry)

		policies := make([]string, 0, len(r.Policy))
		for _, p := range r.Policy {
			policies = append(policies, strings.TrimSpace(string(p)))
		}

		sort.Strings(policies)

		validations := make([]string, 0, len(r.Validation))
		for _, v := range r.Validation {
			validations = append(validations, strings.TrimSpace(string(v)))
		}

		sort.Strings(validations)

		b.WriteString(url)
		b.WriteString(sepField)

		for i, p := range policies {
			if i > 0 {
				b.WriteString(sepList)
			}

			b.WriteString(p)
		}

		b.WriteString(sepField)

		for i, v := range validations {
			if i > 0 {
				b.WriteString(sepList)
			}

			b.WriteString(v)
		}

		b.WriteString(sepRule)
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}

// Has is useful in tests and debugging.
func (c *RegistryRuleSetCache) Has(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.rs[id]

	return ok
}

// InsertForTest can be behind a build tag if you prefer, but it's fine to keep simple.
//
//nolint:unused
func (c *RegistryRuleSetCache) insertForTest(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rs == nil {
		c.rs = make(map[string]*RuleSet)
	}

	c.rs[id] = &RuleSet{ID: id}
}

func buildRuleSet(id string, specRules []api.OCIRegistry) (*RuleSet, error) {
	rs := &RuleSet{
		ID:       id,
		Compiled: make([]CompiledRule, 0, len(specRules)),
	}

	for _, r := range specRules {
		re, err := regexp.Compile(r.Registry)
		if err != nil {
			return nil, fmt.Errorf("invalid registry regex %q: %w", r.Registry, err)
		}

		cr := CompiledRule{
			Registry: r.Registry,
			RE:       re,
		}

		if len(r.Policy) > 0 {
			cr.AllowedPolicy = make(map[corev1.PullPolicy]struct{}, len(r.Policy))
			for _, p := range r.Policy {
				cr.AllowedPolicy[p] = struct{}{}
			}
		}

		for _, v := range r.Validation {
			switch v {
			case api.ValidateImages:
				cr.ValidateImages = true
				rs.HasImages = true
			case api.ValidateVolumes:
				cr.ValidateVolumes = true
				rs.HasVolumes = true
			}
		}

		rs.Compiled = append(rs.Compiled, cr)
	}

	return rs, nil
}
