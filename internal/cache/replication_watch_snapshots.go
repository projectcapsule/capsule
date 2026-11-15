// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"container/list"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	MaxWatchSnapshotObjects = 1024
	MaxWatchSnapshotBytes   = 1024 * 1024
	MaxWatchObjectBytes     = 512 * 1024
)

type watchSnapshot struct {
	object *unstructured.Unstructured
	key    string
	bytes  int
	stamp  uint64
}

// ReplicationWatchSnapshots owns immutable object snapshots for one watch identity
// and resource scope. Both entries and conservatively accounted bytes are bounded.
// Eviction drops old-state knowledge; it must never fabricate a CREATE or nil old
// object for an UPDATE. Callers must treat supplied and returned objects as read-only.
type ReplicationWatchSnapshots struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
	bytes   int
	closed  bool
}

func NewReplicationWatchSnapshots() *ReplicationWatchSnapshots {
	return &ReplicationWatchSnapshots{entries: make(map[string]*list.Element)}
}

func (c *ReplicationWatchSnapshots) Get(key string) (*unstructured.Unstructured, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, found := c.entries[key]
	if !found {
		return nil, 0, false
	}

	value := watchSnapshotValue(element)

	return value.object, value.bytes, true
}

// Put takes ownership of object and returns the change in retained bytes.
func (c *ReplicationWatchSnapshots) Put(key string, object *unstructured.Unstructured, size int, stamp uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.bytes

	if c.closed {
		return 0
	}

	element := c.entries[key]
	if object == nil || size <= 0 || size > MaxWatchObjectBytes {
		if element != nil {
			c.remove(element)
		}

		return c.bytes - before
	}

	if element != nil {
		value := watchSnapshotValue(element)
		c.bytes -= value.bytes
		value.bytes = 0

		c.order.MoveToFront(element)
	}

	for (element == nil && len(c.entries) >= MaxWatchSnapshotObjects) || c.bytes+size > MaxWatchSnapshotBytes {
		c.remove(c.order.Back())
	}

	if element == nil {
		element = c.order.PushFront(&watchSnapshot{key: key})
		c.entries[key] = element
	}

	value := watchSnapshotValue(element)
	value.object, value.bytes, value.stamp = object, size, stamp
	c.bytes += size

	return c.bytes - before
}

func (c *ReplicationWatchSnapshots) Delete(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.bytes

	if element := c.entries[key]; element != nil {
		c.remove(element)
	}

	return c.bytes - before
}

// Missing returns only retained snapshots absent from a completed LIST. A partial
// baseline must not infer deletions for objects outside its bounded window.
func (c *ReplicationWatchSnapshots) Missing(stamp uint64) []*unstructured.Unstructured {
	c.mu.Lock()
	defer c.mu.Unlock()

	var objects []*unstructured.Unstructured

	for _, element := range c.entries {
		value := watchSnapshotValue(element)
		if value.stamp != stamp {
			objects = append(objects, value.object)
		}
	}

	return objects
}

func (c *ReplicationWatchSnapshots) List() []any {
	c.mu.Lock()
	defer c.mu.Unlock()

	objects := make([]any, 0, len(c.entries))

	for _, element := range c.entries {
		objects = append(objects, watchSnapshotValue(element).object)
	}

	return objects
}

func (c *ReplicationWatchSnapshots) Bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.bytes
}

func (c *ReplicationWatchSnapshots) Close() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.bytes
	c.entries = make(map[string]*list.Element)
	c.order.Init()
	c.bytes = 0
	c.closed = true

	return -before
}

func (c *ReplicationWatchSnapshots) remove(element *list.Element) {
	value := watchSnapshotValue(element)
	delete(c.entries, value.key)
	c.bytes -= value.bytes
	c.order.Remove(element)
}

// WatchObjectBytes bounds traversal and accounts for strings, map entries, slice
// slots and cache bookkeeping without marshaling or copying event objects. It is
// a conservative working-set budget, not an exact Go heap measurement.
func WatchObjectBytes(object *unstructured.Unstructured) int {
	if object == nil {
		return 0
	}

	return 512 + watchValueBytes(object.Object, MaxWatchObjectBytes-512)
}

func watchValueBytes(value any, limit int) int {
	if limit <= 0 {
		return limit + 1
	}

	size := 32

	switch v := value.(type) {
	case nil, bool, int64, float64:
	case string:
		if len(v) > limit/2 {
			return limit + 1
		}

		size += 2 * len(v)
	case map[string]any:
		size = 256
		for key, item := range v {
			size += 128 + len(key)
			if size > limit {
				return limit + 1
			}

			size += watchValueBytes(item, limit-size)
			if size > limit {
				return limit + 1
			}
		}
	case []any:
		for _, item := range v {
			size += 32
			if size > limit {
				return limit + 1
			}

			size += watchValueBytes(item, limit-size)
			if size > limit {
				return limit + 1
			}
		}
	default:
		return limit + 1
	}

	return min(size, limit+1)
}

func watchSnapshotValue(element *list.Element) *watchSnapshot {
	value, ok := element.Value.(*watchSnapshot)
	if !ok {
		panic("invalid private watch snapshot cache entry")
	}

	return value
}
