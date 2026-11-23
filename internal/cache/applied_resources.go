// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"container/list"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

const maxAppliedResources = 32768

// AppliedResources bounds both retained memory and the lifetime of a no-op
// shortcut. Eviction only causes another ordinary, authoritative apply.
type AppliedResources[K comparable, V any] struct {
	mu        sync.Mutex
	entries   map[K]*list.Element
	order     list.List
	now       func() time.Time
	copyValue func(V) V
}

type appliedResource[K comparable, V any] struct {
	key     K
	uid     types.UID
	version string
	result  V
	expires time.Time
}

func NewAppliedResources[K comparable, V any](copyValue func(V) V) *AppliedResources[K, V] {
	return &AppliedResources[K, V]{copyValue: copyValue, entries: make(map[K]*list.Element), now: time.Now}
}

func (c *AppliedResources[K, V]) Get(key K, uid types.UID, version string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.entries[key]
	if e == nil {
		var zero V

		return zero, false
	}

	entry := e.Value.(appliedResource[K, V]) //nolint:forcetypeassert
	if uid == "" || version == "" || entry.uid != uid || entry.version != version || !c.now().Before(entry.expires) {
		delete(c.entries, key)
		c.order.Remove(e)

		var zero V

		return zero, false
	}

	c.order.MoveToFront(e)

	result := entry.result
	result = c.copyValue(result)

	return result, true
}

func (c *AppliedResources[K, V]) Set(key K, uid types.UID, version string, result V) {
	if uid == "" || version == "" {
		return
	}

	result = c.copyValue(result)
	entry := appliedResource[K, V]{key: key, uid: uid, version: version, result: result, expires: c.now().Add(5 * time.Minute)}

	c.mu.Lock()
	defer c.mu.Unlock()

	if e := c.entries[key]; e != nil {
		e.Value = entry
		c.order.MoveToFront(e)

		return
	}

	if len(c.entries) >= maxAppliedResources {
		last := c.order.Back()
		delete(c.entries, last.Value.(appliedResource[K, V]).key) //nolint:forcetypeassert
		c.order.Remove(last)
	}

	c.entries[key] = c.order.PushFront(entry)
}
