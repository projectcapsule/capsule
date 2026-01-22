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

type NamespaceRegistriesCache struct {
	mu sync.RWMutex

	byNamespace map[string]*RuleSet // ns -> ruleset pointer

	ruleSets map[string]*RuleSet // id -> unique ruleset
	refCount map[string]int      // id -> number of namespaces referencing
}

func NewNamespaceRegistriesCache() *NamespaceRegistriesCache {
	return &NamespaceRegistriesCache{
		byNamespace: map[string]*RuleSet{},
		ruleSets:    map[string]*RuleSet{},
		refCount:    map[string]int{},
	}
}

// Set builds (or reuses) a ruleset from specRules and assigns it to the namespace.
func (c *NamespaceRegistriesCache) Set(namespace string, specRules []api.OCIRegistry) error {
	rs, err := buildRuleSet(specRules)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Dedup by ruleset ID
	if existing, ok := c.ruleSets[rs.ID]; ok {
		rs = existing
	} else {
		c.ruleSets[rs.ID] = rs
		c.refCount[rs.ID] = 0
	}

	// Adjust old reference if needed
	if old, ok := c.byNamespace[namespace]; ok && old != nil {
		if old.ID == rs.ID {
			return nil
		}

		c.refCount[old.ID]--
		if c.refCount[old.ID] <= 0 {
			delete(c.refCount, old.ID)
			delete(c.ruleSets, old.ID)
		}
	}

	c.byNamespace[namespace] = rs
	c.refCount[rs.ID]++

	return nil
}

func (c *NamespaceRegistriesCache) Delete(namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	old, ok := c.byNamespace[namespace]
	if !ok || old == nil {
		return
	}

	delete(c.byNamespace, namespace)

	c.refCount[old.ID]--
	if c.refCount[old.ID] <= 0 {
		delete(c.refCount, old.ID)
		delete(c.ruleSets, old.ID)
	}
}

func (c *NamespaceRegistriesCache) Get(namespace string) (*RuleSet, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rs, ok := c.byNamespace[namespace]

	return rs, ok && rs != nil
}

func (c *NamespaceRegistriesCache) Stats() (namespaces int, uniqueRuleSets int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.byNamespace), len(c.ruleSets)
}

func buildRuleSet(specRules []api.OCIRegistry) (*RuleSet, error) {
	id := hashRules(specRules)

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

func hashRules(specRules []api.OCIRegistry) string {
	// IMPORTANT: preserve rule order (later wins)
	var b strings.Builder

	b.Grow(len(specRules) * 64)

	sepRule := "\n"
	sepField := "\x1f"
	sepList := "\x1e"

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
