// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

type PendingDeleteHint struct {
	UID       types.UID
	CreatedAt time.Time
}

type QuantityEntry struct {
	Reserved       resource.Quantity
	PendingDeletes []PendingDeleteHint
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAccess     time.Time
}

type QuantityCache[K comparable] struct {
	mu    sync.RWMutex
	data  map[K]QuantityEntry
	clock func() time.Time
}

func NewQuantityCache[K comparable]() *QuantityCache[K] {
	return &QuantityCache[K]{
		data:  make(map[K]QuantityEntry),
		clock: time.Now,
	}
}

func (c *QuantityCache[K]) Get(key K) (QuantityEntry, bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()
	if !ok {
		return QuantityEntry{}, false
	}

	return copyEntry(entry), true
}

func (c *QuantityCache[K]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.data[key]
	if ok {
		delete(c.data, key)
	}

	return ok
}

func (c *QuantityCache[K]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

func (c *QuantityCache[K]) Snapshot() map[K]QuantityEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[K]QuantityEntry, len(c.data))
	for k, v := range c.data {
		out[k] = copyEntry(v)
	}

	return out
}

func (c *QuantityCache[K]) PurgeOlderThan(ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}

	threshold := c.clock().Add(-ttl)

	c.mu.Lock()
	defer c.mu.Unlock()

	deleted := 0
	for k, v := range c.data {
		if v.UpdatedAt.Before(threshold) {
			delete(c.data, k)
			deleted++
		}
	}

	return deleted
}

// CheckAndReserve atomically validates:
//
//	persistedUsed + inflightReserved + delta <= limit
//
// If valid, delta is added to Reserved.
func (c *QuantityCache[K]) CheckAndReserve(
	key K,
	persistedUsed resource.Quantity,
	limit resource.Quantity,
	delta resource.Quantity,
) (allowed bool, effectiveUsed resource.Quantity, entry QuantityEntry) {
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists := c.data[key]

	inflight := resource.MustParse("0")
	if exists {
		inflight = current.Reserved.DeepCopy()
	}

	effectiveUsed = persistedUsed.DeepCopy()
	effectiveUsed.Add(inflight)

	newUsed := effectiveUsed.DeepCopy()
	newUsed.Add(delta)

	if newUsed.Cmp(limit) > 0 {
		return false, effectiveUsed, copyEntry(current)
	}

	if !exists {
		current = QuantityEntry{
			Reserved:   delta.DeepCopy(),
			CreatedAt:  now,
			UpdatedAt:  now,
			LastAccess: now,
		}
	} else {
		current.Reserved.Add(delta)
		current.UpdatedAt = now
		current.LastAccess = now
	}

	c.data[key] = current

	return true, newUsed, copyEntry(current)
}

// Release subtracts delta from Reserved.
// If the result reaches zero and there are no pending deletes, the entry is deleted.
func (c *QuantityCache[K]) Release(key K, delta resource.Quantity) bool {
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.data[key]
	if !ok {
		return false
	}

	current.Reserved.Sub(delta)
	current.UpdatedAt = now
	current.LastAccess = now

	if current.Reserved.IsZero() && len(current.PendingDeletes) == 0 {
		delete(c.data, key)
		return true
	}

	c.data[key] = current
	return true
}

// AddPendingDelete registers a UID that should disappear from the rebuilt claims.
func (c *QuantityCache[K]) AddPendingDelete(key K, uid types.UID) {
	if uid == "" {
		return
	}

	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists := c.data[key]
	if !exists {
		current = QuantityEntry{
			Reserved:  resource.MustParse("0"),
			CreatedAt: now,
		}
	}

	if !containsPendingDelete(current.PendingDeletes, uid) {
		current.PendingDeletes = append(current.PendingDeletes, PendingDeleteHint{
			UID:       uid,
			CreatedAt: now,
		})
	}

	current.UpdatedAt = now
	current.LastAccess = now
	c.data[key] = current
}

// RemovePendingDelete removes one deletion hint.
// If the entry becomes empty, it is deleted.
func (c *QuantityCache[K]) RemovePendingDelete(key K, uid types.UID) bool {
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.data[key]
	if !ok {
		return false
	}

	idx := indexPendingDelete(current.PendingDeletes, uid)
	if idx == -1 {
		return false
	}

	current.PendingDeletes = slices.Delete(current.PendingDeletes, idx, idx+1)
	current.UpdatedAt = now
	current.LastAccess = now

	if current.Reserved.IsZero() && len(current.PendingDeletes) == 0 {
		delete(c.data, key)
		return true
	}

	c.data[key] = current
	return true
}

// PendingDeletes returns a copy of pending delete UIDs for the key.
func (c *QuantityCache[K]) PendingDeletes(key K) []types.UID {
	c.mu.RLock()
	defer c.mu.RUnlock()

	current, ok := c.data[key]
	if !ok || len(current.PendingDeletes) == 0 {
		return nil
	}

	out := make([]types.UID, 0, len(current.PendingDeletes))
	for _, hint := range current.PendingDeletes {
		out = append(out, hint.UID)
	}

	return out
}

// HasPendingDeletes reports whether the key has any deletion hints.
func (c *QuantityCache[K]) HasPendingDeletes(key K) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	current, ok := c.data[key]
	return ok && len(current.PendingDeletes) > 0
}

func copyEntry(in QuantityEntry) QuantityEntry {
	out := QuantityEntry{
		Reserved:   in.Reserved.DeepCopy(),
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.UpdatedAt,
		LastAccess: in.LastAccess,
	}

	if len(in.PendingDeletes) > 0 {
		out.PendingDeletes = make([]PendingDeleteHint, len(in.PendingDeletes))
		copy(out.PendingDeletes, in.PendingDeletes)
	}

	return out
}

func containsPendingDelete(in []PendingDeleteHint, uid types.UID) bool {
	return indexPendingDelete(in, uid) != -1
}

func indexPendingDelete(in []PendingDeleteHint, uid types.UID) int {
	for i := range in {
		if in[i].UID == uid {
			return i
		}
	}
	return -1
}
