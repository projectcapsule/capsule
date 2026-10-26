// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

type cleanupDiscovery struct {
	discovery.DiscoveryInterface
	resources []*metav1.APIResourceList
	err       error
	calls     atomic.Int32
	build     func()
}

func (d *cleanupDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	d.calls.Add(1)
	if d.build != nil {
		d.build()
	}
	return d.resources, d.err
}

func cleanupDiscoveryResources(count int) []*metav1.APIResourceList {
	list := &metav1.APIResourceList{GroupVersion: "example.com/v1"}
	for i := range count {
		list.APIResources = append(list.APIResources, metav1.APIResource{
			Name: fmt.Sprintf("objects%d", i), Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"},
		})
	}
	return []*metav1.APIResourceList{list}
}

func TestDiscoveryCleanupCacheSnapshots(t *testing.T) {
	disco := &cleanupDiscovery{resources: cleanupDiscoveryResources(2)}
	cache := NewDiscoveryNamespacedResourceCache()
	first, err := cache.Get(disco)
	if err != nil || len(first) != 2 {
		t.Fatalf("discovery=%v error=%v", first, err)
	}
	first[0].Resource = "caller-mutation"
	warm, err := cache.Get(disco)
	if err != nil || warm[0].Resource != "objects0" || disco.calls.Load() != 1 {
		t.Fatalf("warm=%v error=%v calls=%d", warm, err, disco.calls.Load())
	}
	cache.Invalidate()
	disco.resources = cleanupDiscoveryResources(1)
	fresh, err := cache.Get(disco)
	if err != nil || len(fresh) != 1 || len(warm) != 2 || warm[0].Resource != "objects0" {
		t.Fatalf("refresh=%v old=%v error=%v", fresh, warm, err)
	}
	cache.mu.Lock()
	cache.expiresAt = time.Time{}
	cache.mu.Unlock()
	if _, err := cache.Get(disco); err != nil || disco.calls.Load() != 3 {
		t.Fatalf("expiry: error=%v calls=%d", err, disco.calls.Load())
	}
}

func TestDiscoveryCleanupCacheEmptyAndErrors(t *testing.T) {
	for _, mode := range []string{"empty", "failed", "partial", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			disco := &cleanupDiscovery{}
			switch mode {
			case "failed":
				disco.err = errors.New("discovery unavailable")
			case "partial":
				disco.resources = cleanupDiscoveryResources(1)
				disco.err = errors.New("one API unavailable")
			case "malformed":
				disco.resources = []*metav1.APIResourceList{{GroupVersion: "invalid/group/version"}}
			}
			cache := NewDiscoveryNamespacedResourceCache()
			for range 2 {
				gvrs, err := cache.Get(disco)
				if (err != nil) != (mode == "failed" || mode == "malformed") || (mode == "partial" && len(gvrs) != 1) {
					t.Fatalf("result=%v error=%v", gvrs, err)
				}
			}
			wantCalls := int32(1)
			if mode == "failed" || mode == "malformed" {
				wantCalls = 2
			}
			if disco.calls.Load() != wantCalls {
				t.Fatalf("calls=%d want=%d", disco.calls.Load(), wantCalls)
			}
		})
	}
}

func TestDiscoveryCleanupCacheConcurrentMisses(t *testing.T) {
	disco := &cleanupDiscovery{resources: cleanupDiscoveryResources(30)}
	cache := NewDiscoveryNamespacedResourceCache()
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			gvrs, err := cache.Get(disco)
			if err != nil || len(gvrs) != 30 {
				t.Errorf("discovery=%v error=%v", gvrs, err)
			}
			if len(gvrs) > 0 {
				gvrs[0].Resource = "private-mutation"
			}
		})
	}
	group.Wait()
	if disco.calls.Load() != 1 {
		t.Fatalf("concurrent misses performed %d discovery calls", disco.calls.Load())
	}
}

func TestDiscoveryCleanupCacheInvalidatesDuringBuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	disco := &cleanupDiscovery{resources: cleanupDiscoveryResources(1), build: func() { close(started); <-release }}
	cache := NewDiscoveryNamespacedResourceCache()
	done := make(chan error, 1)
	go func() { _, err := cache.Get(disco); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("discovery did not start")
	}
	// Invalidation must not block behind network I/O or let that in-flight
	// discovery republish an entry invalidated by an API change.
	invalidated := make(chan struct{})
	go func() { cache.Invalidate(); close(invalidated) }()
	select {
	case <-invalidated:
	case <-ctx.Done():
		t.Fatal("cache invalidation waited for network I/O")
	}
	released.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	disco.build = nil
	disco.resources = cleanupDiscoveryResources(2)
	gvrs, err := cache.Get(disco)
	if err != nil || len(gvrs) != 2 || disco.calls.Load() != 2 {
		t.Fatalf("refresh=%v error=%v calls=%d", gvrs, err, disco.calls.Load())
	}
}

func BenchmarkDiscoveryCleanupCache(b *testing.B) {
	for _, count := range []int{30, 300} {
		for _, mode := range []string{"cold", "warm", "invalidated", "parallel"} {
			b.Run(fmt.Sprintf("resources=%d/%s", count, mode), func(b *testing.B) {
				disco := &cleanupDiscovery{resources: cleanupDiscoveryResources(count)}
				cache := NewDiscoveryNamespacedResourceCache()
				want, err := cache.Get(disco)
				if err != nil {
					b.Fatal(err)
				}
				disco.calls.Store(0)
				check := func() {
					result, err := cache.Get(disco)
					if err != nil || len(result) != len(want) || result[len(result)-1] != want[len(want)-1] {
						b.Fatalf("result=%v error=%v", result, err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				if mode == "parallel" {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							check()
						}
					})
				} else {
					for b.Loop() {
						if mode == "cold" {
							cache = NewDiscoveryNamespacedResourceCache()
						}
						if mode == "invalidated" {
							cache.Invalidate()
						}
						check()
					}
				}
				b.ReportMetric(float64(disco.calls.Load())/float64(b.N), "discovery/op")
			})
		}
	}
}
