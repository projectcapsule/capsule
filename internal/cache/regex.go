// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/projectcapsule/capsule/pkg/api"
)

type CompiledRegex struct {
	ID         string
	Expression string
	Negate     bool
	RE         *regexp.Regexp
}

func (r *CompiledRegex) MatchString(value string) bool {
	if r == nil || r.RE == nil {
		return false
	}

	matched := r.RE.MatchString(value)

	if r.Negate {
		return !matched
	}

	return matched
}

type RegexCache struct {
	mu sync.RWMutex
	re map[string]*CompiledRegex
}

func NewRegexCache() *RegexCache {
	return &RegexCache{
		re: make(map[string]*CompiledRegex),
	}
}

func (c *RegexCache) GetOrCompile(expr api.RegExpression) (*CompiledRegex, bool, error) {
	if c == nil {
		return nil, false, fmt.Errorf("regex cache is nil")
	}

	expression := strings.TrimSpace(expr.Expression)
	if expression == "" {
		return nil, false, fmt.Errorf("regex expression must not be empty")
	}

	id := HashRegex(expr)

	c.mu.RLock()
	compiled := c.re[id]
	c.mu.RUnlock()

	if compiled != nil {
		return compiled, true, nil
	}

	re, err := regexp.Compile(expression)
	if err != nil {
		return nil, false, fmt.Errorf("invalid regex expression %q: %w", expression, err)
	}

	built := &CompiledRegex{
		ID:         id,
		Expression: expression,
		Negate:     expr.Negate,
		RE:         re,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.re == nil {
		c.re = make(map[string]*CompiledRegex)
	}

	if compiled = c.re[id]; compiled != nil {
		return compiled, true, nil
	}

	c.re[id] = built

	return built, false, nil
}

func (c *RegexCache) Has(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.re[id]

	return ok
}

func (c *RegexCache) Stats() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.re)
}

func (c *RegexCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.re = make(map[string]*CompiledRegex)
}

func HashRegex(expr api.RegExpression) string {
	var b strings.Builder

	b.WriteString(strings.TrimSpace(expr.Expression))
	b.WriteString("\x1f")

	if expr.Negate {
		b.WriteString("1")
	} else {
		b.WriteString("0")
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}
