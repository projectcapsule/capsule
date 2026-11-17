// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func snapshotObject(name, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": name, "namespace": "tenant-a", "uid": "one"}, "data": map[string]any{"value": value}}}
}

func TestWatchSnapshotsBoundEvictionAndIdentityIsolation(t *testing.T) {
	a, b := NewReplicationWatchSnapshots(), NewReplicationWatchSnapshots()
	first := snapshotObject("first", "original")
	a.Put("first", first, WatchObjectBytes(first), 1)
	b.Put("first", snapshotObject("first", "other tenant"), 2048, 1)
	for i := range 2000 {
		object := snapshotObject(fmt.Sprint(i), strings.Repeat("x", 16*1024))
		a.Put(fmt.Sprint(i), object, WatchObjectBytes(object), 2)
	}
	if _, _, found := a.Get("first"); found {
		t.Fatal("least recently observed object was not evicted")
	}
	if len(a.List()) > MaxWatchSnapshotObjects || a.Bytes() > MaxWatchSnapshotBytes {
		t.Fatal("snapshot capacity exceeded")
	}
	if object, _, found := b.Get("first"); !found || object.Object["data"].(map[string]any)["value"] != "other tenant" {
		t.Fatal("another watch identity was affected")
	}
	if first.Object["data"].(map[string]any)["value"] != "original" {
		t.Fatal("eviction mutated an immutable event snapshot")
	}
	oversized := snapshotObject("large", strings.Repeat("x", MaxWatchObjectBytes))
	if size := WatchObjectBytes(oversized); size <= MaxWatchObjectBytes {
		t.Fatal("oversized payload was accepted")
	} else {
		a.Put("large", oversized, size, 3)
	}
	if _, _, found := a.Get("large"); found {
		t.Fatal("oversized object retained")
	}
}

func TestWatchSnapshotsReplacementMissingAndClose(t *testing.T) {
	c := NewReplicationWatchSnapshots()
	old := snapshotObject("source", "old")
	c.Put("source", old, WatchObjectBytes(old), 1)
	next := snapshotObject("source", "new")
	c.Put("source", next, WatchObjectBytes(next), 2)
	if len(c.Missing(2)) != 0 || len(c.Missing(3)) != 1 {
		t.Fatal("completed baseline membership lost")
	}
	if old.Object["data"].(map[string]any)["value"] != "old" || len(c.List()) != 1 {
		t.Fatal("replacement changed the old snapshot or retained duplicates")
	}
	if c.Close() >= 0 || c.Bytes() != 0 || len(c.List()) != 0 {
		t.Fatal("close did not release snapshots")
	}
	if c.Put("late", next, WatchObjectBytes(next), 3) != 0 || len(c.List()) != 0 {
		t.Fatal("late canceled LIST repopulated a closed stream")
	}
}

func TestWatchSnapshotConcurrentAccessAndClose(t *testing.T) {
	c := NewReplicationWatchSnapshots()
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for i := range 100 {
				key := fmt.Sprintf("%d/%d", worker, i)
				object := snapshotObject(key, "value")
				c.Put(key, object, WatchObjectBytes(object), uint64(i))
				c.Get(key)
				c.Missing(uint64(i))
				c.List()
				c.Bytes()
				c.Delete(key)
			}
		})
	}
	c.Close()
	workers.Wait()
	if c.Bytes() != 0 || len(c.List()) != 0 {
		t.Fatal("closed cache retained concurrent publications")
	}
}

func BenchmarkWatchSnapshotCache(b *testing.B) {
	for _, concurrent := range []bool{false, true} {
		b.Run(fmt.Sprintf("concurrent=%t", concurrent), func(b *testing.B) {
			c := NewReplicationWatchSnapshots()
			object := snapshotObject("source", strings.Repeat("x", 1024))
			b.ReportAllocs()
			run := func() { size := WatchObjectBytes(object); c.Put("source", object, size, 1); c.Get("source") }
			if concurrent {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						run()
					}
				})
			} else {
				for b.Loop() {
					run()
				}
			}
		})
	}
}
