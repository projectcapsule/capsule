// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

const (
	MaxPreparedReplications      = 4096
	MaxPreparedTriggerConditions = 8192
)

type preparedReplication struct {
	generation  int64
	fingerprint [32]byte
	conditions  int
	triggers    *replication.PreparedTriggers
}

// PreparedReplicationTriggers retains only current immutable source projections.
// Readers cannot mutate the published artifacts. Generation/UID checks prevent a
// stale index or delayed event from using an edited or recreated subscription.
type PreparedReplicationTriggers struct {
	mu         sync.RWMutex
	entries    map[types.UID]preparedReplication
	conditions int
}

func NewPreparedReplicationTriggers() *PreparedReplicationTriggers {
	return &PreparedReplicationTriggers{entries: make(map[types.UID]preparedReplication)}
}

// Validate reuses the current prepared subscription on ordinary reconciliation.
// Invalid specifications must surface in resource status even when no admission
// webhook is installed. Cold or omitted subscriptions use the shared CEL cache.
func (c *PreparedReplicationTriggers) Validate(uid types.UID, generation int64, spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler replication.ConditionValidator) error {
	if c != nil {
		if _, ok := c.Get(uid, generation); ok {
			return nil
		}
	}

	return replication.ValidateTriggers(spec, namespaced, compiler)
}

func (c *PreparedReplicationTriggers) Get(uid types.UID, generation int64) (*replication.PreparedTriggers, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.entries[uid]

	return value.triggers, ok && value.generation == generation
}

func (c *PreparedReplicationTriggers) GetOrBuild(uid types.UID, generation int64, conditions int, fingerprint [32]byte, build func() (*replication.PreparedTriggers, error)) (*replication.PreparedTriggers, error) {
	if uid == "" {
		return nil, fmt.Errorf("watch subscription requires a persisted UID")
	}

	if value, ok := c.Get(uid, generation); ok {
		return value, nil
	}

	c.mu.Lock()

	previousValue := c.entries[uid]
	if previousValue.triggers != nil && previousValue.fingerprint == fingerprint && previousValue.generation <= generation {
		previousValue.generation = generation
		c.entries[uid] = previousValue
		c.mu.Unlock()

		return previousValue.triggers, nil
	}
	c.mu.Unlock()
	c.mu.RLock()
	previous := c.entries[uid]
	full := (previous.triggers == nil && len(c.entries) >= MaxPreparedReplications) || c.conditions-previous.conditions+conditions > MaxPreparedTriggerConditions
	c.mu.RUnlock()

	if full {
		return nil, fmt.Errorf("replication trigger preparation budget exhausted")
	}

	value, err := build()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous = c.entries[uid]
	if previous.triggers != nil && previous.generation >= generation {
		if previous.generation == generation {
			return previous.triggers, nil
		}

		return nil, fmt.Errorf("stale watch subscription generation")
	}

	if (previous.triggers == nil && len(c.entries) >= MaxPreparedReplications) || c.conditions-previous.conditions+conditions > MaxPreparedTriggerConditions {
		return nil, fmt.Errorf("replication trigger preparation budget exhausted")
	}

	c.conditions += conditions - previous.conditions
	c.entries[uid] = preparedReplication{generation: generation, fingerprint: fingerprint, conditions: conditions, triggers: value}

	return value, nil
}

func (c *PreparedReplicationTriggers) PruneActive(active map[types.UID]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for uid, value := range c.entries {
		if _, ok := active[uid]; !ok {
			c.conditions -= value.conditions
			delete(c.entries, uid)
		}
	}
}

func (c *PreparedReplicationTriggers) Delete(uid types.UID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if value, ok := c.entries[uid]; ok {
		c.conditions -= value.conditions
		delete(c.entries, uid)
	}
}
