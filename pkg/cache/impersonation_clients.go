// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils/users"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Key struct {
	Namespace string
	Name      string
}

type ImpersonationCache struct {
	mu      sync.RWMutex
	clients map[Key]client.Client

	baseMu   sync.RWMutex
	baseREST *rest.Config
}

func NewImpersonationCache() *ImpersonationCache {
	return &ImpersonationCache{
		clients: make(map[Key]client.Client),
	}
}

// Get returns a cached client if present.
func (c *ImpersonationCache) Get(namespace, name string) (client.Client, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cl, ok := c.clients[Key{Namespace: namespace, Name: name}]
	return cl, ok && cl != nil
}

// Set stores a client explicitly (rarely needed).
func (c *ImpersonationCache) Set(namespace, name string, cl client.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[Key{Namespace: namespace, Name: name}] = cl
}

// Invalidate removes one entry.
func (c *ImpersonationCache) Invalidate(namespace, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.clients, Key{Namespace: namespace, Name: name})
}

// Clear drops all cached clients.
func (c *ImpersonationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients = make(map[Key]client.Client)
}

// Stats helps you log cache state.
func (c *ImpersonationCache) Stats() (entries int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.clients)
}

// LoadOrCreate returns a cached impersonated client for the given service account,
// creating and caching it if missing.
//
// - baseRESTProvider should return a base rest.Config used for impersonation.
// - schemeClient is only used to get the Scheme() for building the new client.
func (c *ImpersonationCache) LoadOrCreate(
	ctx context.Context,
	log logr.Logger,
	baseREST *rest.Config,
	scheme *runtime.Scheme,
	sa meta.NamespacedRFC1123ObjectReferenceWithNamespace,
) (client.Client, error) {
	key := Key{Namespace: string(sa.Namespace), Name: string(sa.Name)}

	// Fast path
	if cl, ok := c.Get(key.Namespace, key.Name); ok {
		return cl, nil
	}

	cl, err := users.ImpersonatedKubernetesClientForServiceAccount(
		baseREST,
		scheme,
		sa,
	)
	if err != nil {
		log.Error(err, "failed to create impersonated client", "namespace", key.Namespace, "name", key.Name)
		return nil, err
	}

	// Store (double-check to avoid duplicate creation races)
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.clients[key]; existing != nil {
		return existing, nil
	}
	c.clients[key] = cl
	return cl, nil
}
