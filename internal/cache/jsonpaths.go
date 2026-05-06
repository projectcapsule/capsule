// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"

	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
)

type JSONPathCache struct {
	mu   sync.RWMutex
	data map[string]*jsonpath.CompiledJSONPath
}

func NewJSONPathCache() *JSONPathCache {
	return &JSONPathCache{
		data: make(map[string]*jsonpath.CompiledJSONPath),
	}
}

func (c *JSONPathCache) Get(path string) (*jsonpath.CompiledJSONPath, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	v, ok := c.data[path]

	return v, ok
}

func (c *JSONPathCache) GetOrCompile(path string) (*jsonpath.CompiledJSONPath, error) {
	c.mu.RLock()
	compiled, ok := c.data[path]
	c.mu.RUnlock()

	if ok {
		return compiled, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if compiled, ok = c.data[path]; ok {
		return compiled, nil
	}

	var err error

	compiled, err = jsonpath.CompileJSONPath(path)
	if err != nil {
		return nil, err
	}

	c.data[path] = compiled

	return compiled, nil
}

func (c *JSONPathCache) Delete(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.data[path]; !ok {
		return false
	}

	delete(c.data, path)

	return true
}

func (c *JSONPathCache) DeleteMany(expressions ...string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	deleted := 0

	for _, expr := range expressions {
		if expr == "" {
			continue
		}

		if _, ok := c.data[expr]; ok {
			delete(c.data, expr)

			deleted++
		}
	}

	return deleted
}

func (c *JSONPathCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]*jsonpath.CompiledJSONPath)
}

func (c *JSONPathCache) Stats() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

func (c *JSONPathCache) PruneActive(active map[string]struct{}) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	pruned := 0

	for path := range c.data {
		if _, ok := active[path]; !ok {
			delete(c.data, path)

			pruned++
		}
	}

	return pruned
}
