// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	clientcache "k8s.io/client-go/tools/cache"

	"github.com/projectcapsule/capsule/internal/cache"
)

func BenchmarkWatchSnapshotRetention(b *testing.B) {
	for _, bounded := range []bool{false, true} {
		for _, identities := range []int{1, 8, 64} {
			if !bounded && identities == 64 {
				continue
			}
			for _, count := range []int{100, 1000} {
				b.Run(fmt.Sprintf("bounded=%t/identities=%d/objects=%d", bounded, identities, count), func(b *testing.B) {
					b.ReportAllocs()
					var retained uint64
					var entries int
					for range b.N {
						runtime.GC()
						var before, after runtime.MemStats
						runtime.ReadMemStats(&before)
						stores := make([]any, 0, identities)
						entries = 0
						for identity := range identities {
							var old clientcache.Store
							var snapshots *cache.ReplicationWatchSnapshots
							if bounded {
								snapshots = cache.NewReplicationWatchSnapshots()
							} else {
								old = clientcache.NewStore(clientcache.MetaNamespaceKeyFunc)
							}
							for index := range count {
								object := sourceObject("a")
								object.SetName(fmt.Sprintf("source-%d", index))
								object.Object["data"] = map[string]any{"value": strings.Repeat("x", 16*1024) + fmt.Sprint(identity, index)}
								if bounded {
									snapshots.Put(object.GetName(), object, cache.WatchObjectBytes(object), 1)
								} else if err := old.Add(object); err != nil {
									b.Fatal(err)
								}
							}
							if bounded {
								stores = append(stores, snapshots)
								entries += len(snapshots.List())
							} else {
								stores = append(stores, old)
								entries += len(old.List())
							}
						}
						runtime.GC()
						runtime.ReadMemStats(&after)
						retained = after.HeapAlloc - before.HeapAlloc
						runtime.KeepAlive(stores)
					}
					b.ReportMetric(float64(retained), "retained-B")
					b.ReportMetric(float64(entries), "retained-objects")
				})
			}
		}
	}
}

func BenchmarkWatchEnqueueContention(b *testing.B) {
	f := watchFixtureFor(b, 0)
	object := sourceObject("a")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f.watcher.enqueueEvent(b.Context(), configMapsGVR, resourceWatchIdentity{}, admissionv1.Create, nil, object)
		}
	})
	if f.watcher.eventBytes.Load() > maxWatchEventBytes {
		b.Fatal("event byte budget exceeded")
	}
	for len(f.watcher.events) > 0 {
		f.watcher.releaseEvent(<-f.watcher.events)
	}
}
