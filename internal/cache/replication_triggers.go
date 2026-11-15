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

type failedTriggerValidation struct {
	generation int64
	err        error
}

// PreparedReplicationTriggers retains only current immutable source projections.
// Readers cannot mutate the published artifacts. Generation/UID checks prevent a
// stale index or delayed event from using an edited or recreated subscription.
type PreparedReplicationTriggers struct {
	mu         sync.RWMutex
	entries    map[types.UID]preparedReplication
	conditions int
	failures   map[types.UID]failedTriggerValidation
}

func NewPreparedReplicationTriggers() *PreparedReplicationTriggers {
	return &PreparedReplicationTriggers{entries: make(map[types.UID]preparedReplication), failures: make(map[types.UID]failedTriggerValidation)}
}

// Validate reuses the current prepared subscription on ordinary reconciliation.
// Invalid specifications must surface in resource status even when no admission
// webhook is installed. Cold or omitted subscriptions use the shared CEL cache.
func (c *PreparedReplicationTriggers) Validate(uid types.UID, generation int64, spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler replication.ConditionValidator) error {
	if c != nil {
		if _, ok := c.Get(uid, generation); ok {
			return nil
		}

		if err := c.validationFailure(uid, generation); err != nil {
			return err
		}
	}

	err := replication.ValidateTriggers(spec, namespaced, compiler)
	if err != nil && c != nil {
		c.rememberValidationFailure(uid, generation, err)
	} else if c != nil {
		c.mu.Lock()
		if failure := c.failures[uid]; failure.generation <= generation {
			delete(c.failures, uid)
		}
		c.mu.Unlock()
	}

	return err
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

	if err := c.validationFailure(uid, generation); err != nil {
		return nil, err
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
		c.rememberValidationFailure(uid, generation, err)

		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous = c.entries[uid]

	if failure := c.failures[uid]; failure.generation >= generation && failure.err != nil {
		return nil, failure.err
	}

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
	delete(c.failures, uid)

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

	for uid, failure := range c.failures {
		if generation, ok := active[uid]; !ok || generation != failure.generation {
			delete(c.failures, uid)
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

	delete(c.failures, uid)
}

func (c *PreparedReplicationTriggers) validationFailure(uid types.UID, generation int64) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value := c.failures[uid]
	if value.generation == generation {
		return value.err
	}

	return nil
}

// Publish failures like prepared artifacts: compile outside the shared lock,
// then fence publication against newer generations. The bounded failure set
// retains no source objects or evaluation results.
func (c *PreparedReplicationTriggers) rememberValidationFailure(uid types.UID, generation int64, err error) {
	if uid == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous, exists := c.failures[uid]

	prepared := c.entries[uid]

	if previous.generation > generation || prepared.generation > generation {
		return
	}

	if prepared.triggers != nil {
		c.conditions -= prepared.conditions
		delete(c.entries, uid)
	}

	if !exists && len(c.failures) >= MaxPreparedReplications {
		return
	}

	if c.failures == nil {
		c.failures = make(map[types.UID]failedTriggerValidation)
	}

	c.failures[uid] = failedTriggerValidation{generation: generation, err: err}
}
