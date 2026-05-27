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

type Reservation struct {
	ID        string
	Usage     resource.Quantity
	UID       types.UID
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type QuantityEntry struct {
	Reserved       resource.Quantity
	Reservations   map[string]Reservation
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

// UpsertReservation ensures a reservation is idempotent per (key, reservationID).
// It validates:
//
//	persistedUsed + sum(all reservations including this one) <= limit
func (c *QuantityCache[K]) UpsertReservation(
	key K,
	reservation Reservation,
	persistedUsed resource.Quantity,
	limit resource.Quantity,
) (allowed bool, effectiveUsed resource.Quantity, entry QuantityEntry) {
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, exists := c.data[key]
	if !exists {
		current = QuantityEntry{
			Reserved:     resource.MustParse("0"),
			Reservations: make(map[string]Reservation),
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccess:   now,
		}
	} else if current.Reservations == nil {
		current.Reservations = make(map[string]Reservation)
	}

	previous, hadPrevious := current.Reservations[reservation.ID]

	// Fast path: same reservation, same usage and identity.
	if hadPrevious &&
		previous.Usage.Cmp(reservation.Usage) == 0 &&
		previous.UID == reservation.UID &&
		previous.Group == reservation.Group &&
		previous.Version == reservation.Version &&
		previous.Kind == reservation.Kind &&
		previous.Namespace == reservation.Namespace &&
		previous.Name == reservation.Name {
		current.LastAccess = now
		c.data[key] = current

		effectiveUsed = persistedUsed.DeepCopy()
		effectiveUsed.Add(current.Reserved)

		if effectiveUsed.Sign() < 0 {
			effectiveUsed = resource.MustParse("0")
		}

		return true, effectiveUsed, copyEntry(current)
	}

	candidateReservations := make(map[string]Reservation, len(current.Reservations)+1)
	for id, r := range current.Reservations {
		candidateReservations[id] = Reservation{
			ID:        r.ID,
			Usage:     r.Usage.DeepCopy(),
			UID:       r.UID,
			Group:     r.Group,
			Version:   r.Version,
			Kind:      r.Kind,
			Namespace: r.Namespace,
			Name:      r.Name,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}

	reservation.CreatedAt = func() time.Time {
		if hadPrevious {
			return previous.CreatedAt
		}

		return now
	}()
	reservation.UpdatedAt = now

	candidateReservations[reservation.ID] = reservation

	newReserved := sumReservations(candidateReservations)

	newUsed := persistedUsed.DeepCopy()
	newUsed.Add(newReserved)

	if newUsed.Sign() < 0 {
		newUsed = resource.MustParse("0")
	}

	if newUsed.Cmp(limit) > 0 {
		effectiveUsed = persistedUsed.DeepCopy()
		effectiveUsed.Add(current.Reserved)

		if effectiveUsed.Sign() < 0 {
			effectiveUsed = resource.MustParse("0")
		}

		return false, effectiveUsed, copyEntry(current)
	}

	current.Reservations = candidateReservations
	current.Reserved = newReserved
	current.UpdatedAt = now
	current.LastAccess = now

	if current.Reserved.IsZero() && len(current.PendingDeletes) == 0 {
		delete(c.data, key)

		return true, newUsed, QuantityEntry{}
	}

	c.data[key] = current

	return true, newUsed, copyEntry(current)
}

func (c *QuantityCache[K]) DeleteReservation(key K, reservationID string) bool {
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()

	current, ok := c.data[key]
	if !ok || current.Reservations == nil {
		return false
	}

	if _, exists := current.Reservations[reservationID]; !exists {
		return false
	}

	delete(current.Reservations, reservationID)
	current.Reserved = sumReservations(current.Reservations)
	current.UpdatedAt = now
	current.LastAccess = now

	if current.Reserved.IsZero() && len(current.PendingDeletes) == 0 {
		delete(c.data, key)

		return true
	}

	c.data[key] = current

	return true
}

// PurgeReservationsForKey removes reservations for which shouldDelete returns true.
// Returns number of removed reservations.
func (c *QuantityCache[K]) PurgeReservationsForKey(
	key K,
	shouldDelete func(Reservation) bool,
) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.data[key]
	if !ok || len(entry.Reservations) == 0 {
		return 0
	}

	deleted := 0

	for id, r := range entry.Reservations {
		if shouldDelete(r) {
			delete(entry.Reservations, id)

			deleted++
		}
	}

	if deleted == 0 {
		return 0
	}

	now := c.clock()
	entry.Reserved = sumReservations(entry.Reservations)
	entry.UpdatedAt = now
	entry.LastAccess = now

	if entry.Reserved.IsZero() && len(entry.PendingDeletes) == 0 {
		delete(c.data, key)

		return deleted
	}

	c.data[key] = entry

	return deleted
}

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
			Reserved:     resource.MustParse("0"),
			Reservations: make(map[string]Reservation),
			CreatedAt:    now,
		}
	} else if current.Reservations == nil {
		current.Reservations = make(map[string]Reservation)
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

	if len(in.Reservations) > 0 {
		out.Reservations = make(map[string]Reservation, len(in.Reservations))
		for id, r := range in.Reservations {
			out.Reservations[id] = Reservation{
				ID:        r.ID,
				Usage:     r.Usage.DeepCopy(),
				UID:       r.UID,
				Group:     r.Group,
				Version:   r.Version,
				Kind:      r.Kind,
				Namespace: r.Namespace,
				Name:      r.Name,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			}
		}
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

func sumReservations(in map[string]Reservation) resource.Quantity {
	total := resource.MustParse("0")
	for _, r := range in {
		total.Add(r.Usage)
	}

	if total.Sign() < 0 {
		total = resource.MustParse("0")
	}

	return total
}
