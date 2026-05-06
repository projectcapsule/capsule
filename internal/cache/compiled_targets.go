// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

type CompiledTarget struct {
	capsulev1beta2.CustomQuotaStatusTarget

	CompiledPath       *jsonpath.CompiledJSONPath
	CompiledSelectors  []selectors.CompiledSelectorWithFields
	CompiledConditions []*jsonpath.CompiledJSONPath
}

type CompiledTargetsCache[K comparable] struct {
	mu   sync.RWMutex
	data map[K][]CompiledTarget
}

func NewCompiledTargetsCache[K comparable]() *CompiledTargetsCache[K] {
	return &CompiledTargetsCache[K]{
		data: make(map[K][]CompiledTarget),
	}
}

func (c *CompiledTargetsCache[K]) Get(key K) ([]CompiledTarget, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	v, ok := c.data[key]
	if !ok {
		return nil, false
	}

	out := make([]CompiledTarget, len(v))
	copy(out, v)

	return out, true
}

func (c *CompiledTargetsCache[K]) Set(key K, value []CompiledTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]CompiledTarget, len(value))
	copy(out, value)

	c.data[key] = out
}

func (c *CompiledTargetsCache[K]) GetOrBuild(key K, build func() ([]CompiledTarget, error)) ([]CompiledTarget, error) {
	c.mu.RLock()
	v, ok := c.data[key]
	c.mu.RUnlock()

	if ok {
		out := make([]CompiledTarget, len(v))
		copy(out, v)

		return out, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if v, ok := c.data[key]; ok {
		out := make([]CompiledTarget, len(v))
		copy(out, v)

		return out, nil
	}

	built, err := build()
	if err != nil {
		return nil, err
	}

	stored := make([]CompiledTarget, len(built))
	copy(stored, built)

	c.data[key] = stored

	out := make([]CompiledTarget, len(stored))
	copy(out, stored)

	return out, nil
}

func (c *CompiledTargetsCache[K]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.data[key]
	if ok {
		delete(c.data, key)
	}

	return ok
}

func (c *CompiledTargetsCache[K]) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.data)
}

func (c *CompiledTargetsCache[K]) Stats() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

func (c *CompiledTargetsCache[K]) PruneActive(active map[K]struct{}) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	pruned := 0

	for key := range c.data {
		if _, ok := active[key]; !ok {
			delete(c.data, key)

			pruned++
		}
	}

	return pruned
}
