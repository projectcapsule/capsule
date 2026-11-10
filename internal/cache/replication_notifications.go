// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"
	"time"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

const MaxReplicationNotifications = 4096

type notification struct {
	due, last        time.Time
	interval         time.Duration
	pending, running bool
}

// ReplicationNotifications bounds pending work and coalesces bursts. No lock is
// held across API I/O. Idle entries expire after their local cooldown; the
// controller's persisted trigger status enforces the cross-replica interval.
type ReplicationNotifications struct {
	mu      sync.Mutex
	entries map[replication.Target]*notification
}

func NewReplicationNotifications() *ReplicationNotifications {
	return &ReplicationNotifications{entries: make(map[replication.Target]*notification)}
}

func (c *ReplicationNotifications) Notify(target replication.Target, interval time.Duration, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[target]
	if entry == nil {
		if len(c.entries) >= MaxReplicationNotifications {
			return false
		}

		entry = &notification{due: now.Add(250 * time.Millisecond), interval: interval}
		c.entries[target] = entry
	}

	entry.interval = interval
	entry.pending = true

	return true
}

// Next claims at most one notification, keeping worker concurrency bounded.
func (c *ReplicationNotifications) Next(now time.Time) (replication.Target, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for target, entry := range c.entries {
		if entry.running {
			continue
		}

		if !entry.pending {
			if !now.Before(entry.due) {
				delete(c.entries, target)
			}

			continue
		}

		if now.Before(entry.due) {
			continue
		}

		entry.pending = false
		entry.running = true

		return target, true
	}

	return replication.Target{}, false
}

func (c *ReplicationNotifications) Complete(target replication.Target, now time.Time, retry bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[target]
	if entry == nil {
		return
	}

	entry.running = false
	if retry {
		entry.pending = true
		entry.due = now.Add(time.Second)

		return
	}

	entry.last = now
	entry.due = now.Add(entry.interval)
}

// Prune retires deleted, cordoned, and unsubscribed incarnations independently
// of their cooldown, so long intervals cannot retain obsolete queue capacity.
func (c *ReplicationNotifications) Prune(active map[replication.Target]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for target := range c.entries {
		if _, exists := active[target]; !exists {
			delete(c.entries, target)
		}
	}
}
