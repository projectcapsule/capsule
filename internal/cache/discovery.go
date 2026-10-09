// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

const defaultDiscoveryCacheTTL = 30 * time.Second

type DiscoveryNamespacedResourceCache struct {
	mu         sync.Mutex
	buildMu    sync.Mutex
	generation uint64
	expiresAt  time.Time
	gvrs       []schema.GroupVersionResource
	ttl        time.Duration
}

func NewDiscoveryNamespacedResourceCache() DiscoveryNamespacedResourceCache {
	return DiscoveryNamespacedResourceCache{
		ttl: defaultDiscoveryCacheTTL,
	}
}

func NewDiscoveryNamespacedResourceCacheWithTTL(ttl time.Duration) DiscoveryNamespacedResourceCache {
	if ttl <= 0 {
		ttl = defaultDiscoveryCacheTTL
	}

	return DiscoveryNamespacedResourceCache{
		ttl: ttl,
	}
}

func (c *DiscoveryNamespacedResourceCache) Get(
	disco discovery.DiscoveryInterface,
) ([]schema.GroupVersionResource, error) {
	if gvrs, ok := c.cached(); ok {
		return gvrs, nil
	}

	// Serialize cold discovery independently of cache access. Invalidation and
	// warm reads never wait for network I/O while holding the data mutex.
	c.buildMu.Lock()
	defer c.buildMu.Unlock()

	if gvrs, ok := c.cached(); ok {
		return gvrs, nil
	}

	c.mu.Lock()
	generation := c.generation
	c.mu.Unlock()

	resourceLists, err := disco.ServerPreferredNamespacedResources()
	if err != nil && len(resourceLists) == 0 {
		return nil, fmt.Errorf("discover namespaced resources: %w", err)
	}

	gvrs, err := gvk.NamespacedListableResources(resourceLists)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.generation == generation {
		if c.ttl <= 0 {
			c.ttl = defaultDiscoveryCacheTTL
		}

		// Publish a new snapshot: parallel cleanup can still be reading the old
		// result when this entry expires or is invalidated.
		c.gvrs = gvrs
		c.expiresAt = time.Now().Add(c.ttl)
	}

	return slices.Clone(gvrs), nil
}

func (c *DiscoveryNamespacedResourceCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.expiresAt = time.Time{}
	c.gvrs = nil
	c.generation++
}

func (c *DiscoveryNamespacedResourceCache) cached() ([]schema.GroupVersionResource, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !time.Now().Before(c.expiresAt) {
		return nil, false
	}

	return slices.Clone(c.gvrs), true
}
