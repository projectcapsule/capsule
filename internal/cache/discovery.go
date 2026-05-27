// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

const defaultDiscoveryCacheTTL = 30 * time.Second

type DiscoveryNamespacedResourceCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	gvrs      []schema.GroupVersionResource
	ttl       time.Duration
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ttl <= 0 {
		c.ttl = defaultDiscoveryCacheTTL
	}

	if time.Now().Before(c.expiresAt) && len(c.gvrs) > 0 {
		return c.gvrs, nil
	}

	resourceLists, err := disco.ServerPreferredNamespacedResources()
	if err != nil && len(resourceLists) == 0 {
		return nil, fmt.Errorf("discover namespaced resources: %w", err)
	}

	gvrs, err := gvk.NamespacedListableResources(resourceLists)
	if err != nil {
		return nil, err
	}

	c.gvrs = append(c.gvrs[:0], gvrs...)
	c.expiresAt = time.Now().Add(c.ttl)

	return c.gvrs, nil
}

func (c *DiscoveryNamespacedResourceCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.expiresAt = time.Time{}
	c.gvrs = nil
}
