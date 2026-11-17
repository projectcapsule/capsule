// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/users"
)

type Key struct {
	Namespace string
	Name      string
}

type ImpersonationCache struct {
	mu      sync.RWMutex
	clients map[Key]*impersonationClientEntry
	epoch   uint64
}

type impersonationClientEntry struct {
	client     client.Client
	config     impersonationClientConfig
	nextProtos []string
	explicit   bool
}

// Retain values rather than mutable REST configuration pointers or CA slices.
// Controller callbacks (proxy, dial and transport wrappers) retain manager lifetime.
type impersonationClientConfig struct {
	host, username, password, token, tokenFile string
	serverName, caFile, certFile, keyFile      string
	insecure                                   bool
	ca, cert, key                              [sha256.Size]byte
	scheme                                     *runtime.Scheme
}

func impersonationConfig(base *rest.Config, scheme *runtime.Scheme) impersonationClientConfig {
	return impersonationClientConfig{
		host: base.Host, username: base.Username, password: base.Password, token: base.BearerToken, tokenFile: base.BearerTokenFile,
		serverName: base.ServerName, caFile: base.CAFile, certFile: base.CertFile, keyFile: base.KeyFile,
		insecure: base.Insecure,
		ca:       sha256.Sum256(base.CAData), cert: sha256.Sum256(base.CertData), key: sha256.Sum256(base.KeyData), scheme: scheme,
	}
}

func (e *impersonationClientEntry) matches(config impersonationClientConfig, nextProtos []string) bool {
	return e != nil && (e.explicit || (e.config == config && slices.Equal(e.nextProtos, nextProtos)))
}

func NewImpersonationCache() *ImpersonationCache {
	return &ImpersonationCache{
		clients: make(map[Key]*impersonationClientEntry),
	}
}

// Get returns a cached client if present.
func (c *ImpersonationCache) Get(ns, name string) (client.Client, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.clients[Key{Namespace: ns, Name: name}]
	if !ok {
		return nil, false
	}

	return entry.client, true
}

// Set stores an explicit client whose transport lifetime is managed by the caller.
func (c *ImpersonationCache) Set(namespace, name string, cl client.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cl == nil {
		return
	}

	c.epoch++
	c.clients[Key{Namespace: namespace, Name: name}] = &impersonationClientEntry{client: cl, explicit: true}
}

// Invalidate removes one entry.
func (c *ImpersonationCache) Invalidate(namespace, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.clients, Key{Namespace: namespace, Name: name})

	c.epoch++
}

// Clear drops all cached clients.
func (c *ImpersonationCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.clients = make(map[Key]*impersonationClientEntry)
	c.epoch++
}

// Stats helps you log cache state.
func (c *ImpersonationCache) Stats() (entries int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.clients)
}

// LoadOrCreate returns a cached impersonated client for the given service account,
// replacing it when the current credentials or transport trust have changed.
func (c *ImpersonationCache) LoadOrCreate(
	ctx context.Context,
	log logr.Logger,
	baseREST *rest.Config,
	scheme *runtime.Scheme,
	sa meta.NamespacedRFC1123ObjectReferenceWithNamespace,
) (client.Client, error) {
	key := Key{Namespace: string(sa.Namespace), Name: string(sa.Name)}

	if baseREST == nil {
		c.mu.RLock()
		entry := c.clients[key]
		c.mu.RUnlock()

		if entry != nil && entry.explicit {
			return entry.client, nil
		}

		return nil, fmt.Errorf("base impersonation REST configuration is unavailable")
	}

	config := impersonationConfig(baseREST, scheme)

	c.mu.RLock()

	previous, epoch := c.clients[key], c.epoch
	if previous.matches(config, baseREST.NextProtos) {
		c.mu.RUnlock()

		return previous.client, nil
	}

	c.mu.RUnlock()

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

	if existing := c.clients[key]; existing.matches(config, baseREST.NextProtos) {
		return existing.client, nil
	}

	// An invalidation or another transport publication during construction must
	// not be overwritten by this earlier snapshot. The caller can still use its
	// own pinned configuration; a subsequent load rechecks current trust.
	if c.epoch != epoch || c.clients[key] != previous {
		return cl, nil
	}

	c.clients[key] = &impersonationClientEntry{client: cl, config: config, nextProtos: slices.Clone(baseREST.NextProtos)}

	return cl, nil
}
