// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const maxLabelSelectors = 1024

// LabelSelectorCache stores immutable compiled selectors, never resource snapshots
// or match results. Content keys make changed/deleted/recreated PDBs independent
// of informer freshness. A bounded generation retires unused selectors without
// watching resources or retaining an unbounded history of dry-run requests.
type LabelSelectorCache struct {
	mu        sync.RWMutex
	selectors map[string]labels.Selector
}

func NewLabelSelectorCache() *LabelSelectorCache {
	return &LabelSelectorCache{selectors: make(map[string]labels.Selector)}
}

// GetOrCompile returns a read-only selector. Callers must not modify it.
func (c *LabelSelectorCache) GetOrCompile(selector *metav1.LabelSelector) (labels.Selector, error) {
	keyBytes, err := json.Marshal(selector)
	if err != nil {
		return nil, err
	}

	key := string(keyBytes)

	c.mu.RLock()
	compiled := c.selectors[key]
	c.mu.RUnlock()

	if compiled != nil {
		return compiled, nil
	}

	compiled, err = metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing := c.selectors[key]; existing != nil {
		return existing, nil
	}

	if len(c.selectors) >= maxLabelSelectors {
		clear(c.selectors)
	}

	c.selectors[key] = compiled

	return compiled, nil
}
