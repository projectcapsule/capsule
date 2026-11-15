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

type triggerValidation struct {
	generation int64
	err        error
}

// PreparedReplicationTriggers retains only current immutable source projections.
// Readers cannot mutate the published artifacts. Generation/UID checks prevent a
// stale index or delayed event from using an edited or recreated subscription.
type PreparedReplicationTriggers struct {
	mu          sync.RWMutex
	entries     map[types.UID]preparedReplication
	conditions  int
	validations map[types.UID]triggerValidation
}

func NewPreparedReplicationTriggers() *PreparedReplicationTriggers {
	return &PreparedReplicationTriggers{entries: make(map[types.UID]preparedReplication), validations: make(map[types.UID]triggerValidation)}
}

// Validate reuses both prepared subscriptions and generation-scoped validation
// results. Sources omitted by preparation limits must not repeatedly compile CEL.
// No admission webhook is required to surface invalid specifications in status.
func (c *PreparedReplicationTriggers) Validate(uid types.UID, generation int64, spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler replication.ConditionValidator) error {
	if c != nil {
		if _, ok := c.Get(uid, generation); ok {
			return nil
		}

		if err, ok := c.validationResult(uid, generation); ok {
			return err
		}
	}

	err := replication.ValidateTriggers(spec, namespaced, compiler)

	if c != nil {
		c.rememberValidation(uid, generation, err)
	}

	return err
}

func (c *PreparedReplicationTriggers) Get(uid types.UID, generation int64) (*replication.PreparedTriggers, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.entries[uid]

	if c.validations[uid].generation > generation {
		return nil, false
	}

	return value.triggers, ok && value.generation == generation
}

func (c *PreparedReplicationTriggers) GetOrBuild(uid types.UID, generation int64, conditions int, fingerprint [32]byte, build func() (*replication.PreparedTriggers, error)) (*replication.PreparedTriggers, error) {
	if uid == "" {
		return nil, fmt.Errorf("watch subscription requires a persisted UID")
	}

	if value, ok := c.Get(uid, generation); ok {
		return value, nil
	}

	if err, ok := c.validationResult(uid, generation); ok && err != nil {
		return nil, err
	}

	c.mu.Lock()

	if result := c.validations[uid]; result.generation > generation || (result.generation == generation && result.err != nil) {
		c.mu.Unlock()

		if result.err != nil {
			return nil, result.err
		}

		return nil, fmt.Errorf("stale watch subscription generation")
	}

	previousValue := c.entries[uid]
	if previousValue.triggers != nil && previousValue.fingerprint == fingerprint && previousValue.generation <= generation {
		previousValue.generation = generation
		c.entries[uid] = previousValue
		delete(c.validations, uid)
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
		c.rememberValidation(uid, generation, err)

		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous = c.entries[uid]

	if result := c.validations[uid]; result.generation >= generation {
		if result.err != nil {
			return nil, result.err
		}

		if result.generation > generation {
			return nil, fmt.Errorf("stale watch subscription generation")
		}
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
	delete(c.validations, uid)

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

	for uid, result := range c.validations {
		// The API reader can validate a generation before the informer sees it.
		if generation, ok := active[uid]; !ok || generation > result.generation {
			delete(c.validations, uid)
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

	delete(c.validations, uid)
}

func (c *PreparedReplicationTriggers) validationResult(uid types.UID, generation int64) (error, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.validations[uid]

	return value.err, ok && value.generation == generation
}

// Publish validation results like prepared artifacts: compile outside the shared
// lock, then fence against newer generations. Successful results retain no CEL
// programs, source objects or evaluation inputs. Preparation retires redundant
// validation entries, keeping the bounded working set for omitted sources.
func (c *PreparedReplicationTriggers) rememberValidation(uid types.UID, generation int64, err error) {
	if uid == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	previous, exists := c.validations[uid]

	prepared := c.entries[uid]

	if previous.generation > generation || prepared.generation > generation {
		return
	}

	if prepared.triggers != nil {
		if err != nil {
			c.conditions -= prepared.conditions
			delete(c.entries, uid)
		} else if prepared.generation == generation {
			delete(c.validations, uid)

			return
		}
	}

	if !exists && len(c.validations) >= MaxPreparedReplications {
		return
	}

	if c.validations == nil {
		c.validations = make(map[types.UID]triggerValidation)
	}

	c.validations[uid] = triggerValidation{generation: generation, err: err}
}
