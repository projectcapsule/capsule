// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestAppliedResourcesFreshnessAndIsolation(t *testing.T) {
	c := NewAppliedResources[string, []int](func(v []int) []int { return append([]int(nil), v...) })
	now := time.Now()
	c.now = func() time.Time { return now }
	value := []int{1}
	c.Set("tenant-a", "uid-a", "opaque", value)
	value[0] = 2
	got, hit := c.Get("tenant-a", "uid-a", "opaque")
	require.True(t, hit)
	require.Equal(t, []int{1}, got)
	got[0] = 3
	got, hit = c.Get("tenant-a", "uid-a", "opaque")
	require.True(t, hit)
	require.Equal(t, []int{1}, got)
	_, hit = c.Get("tenant-b", "uid-a", "opaque")
	require.False(t, hit)
	for _, identity := range [][2]string{{"replacement", "opaque"}, {"uid-a", "new-version"}, {"", "opaque"}, {"uid-a", ""}} {
		c.Set("tenant-a", "uid-a", "opaque", value)
		_, hit = c.Get("tenant-a", types.UID(identity[0]), identity[1])
		require.False(t, hit)
	}
	c.Set("tenant-a", "uid-a", "opaque", value)
	now = now.Add(5 * time.Minute)
	_, hit = c.Get("tenant-a", "uid-a", "opaque")
	require.False(t, hit, "hits must not extend the authoritative reapply deadline")
}

func TestAppliedResourcesBoundedAndConcurrent(t *testing.T) {
	c := NewAppliedResources[int, int](func(v int) int { return v })
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range maxAppliedResources / 4 {
				key := worker*maxAppliedResources + i
				c.Set(key, "uid", "version", key)
				if value, hit := c.Get(key, "uid", "version"); hit && value != key {
					t.Errorf("cross-key result: %d != %d", value, key)
				}
			}
		})
	}
	wg.Wait()
	require.Len(t, c.entries, maxAppliedResources)
	require.Equal(t, maxAppliedResources, c.order.Len())
	c.Set(-1, "uid", "version", -1)
	value, hit := c.Get(-1, "uid", "version")
	require.True(t, hit)
	require.Equal(t, -1, value)
}

func BenchmarkAppliedResources(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			c := NewAppliedResources[string, int](func(v int) int { return v })
			c.Set("key", "uid", "version", 1)
			b.ReportAllocs()
			if mode == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						c.Get("key", "uid", "version")
					}
				})
				return
			}
			i := 0
			for b.Loop() {
				if mode == "cold" {
					i++
					c.Set(fmt.Sprint(i), "uid", "version", 1)
					c.Get("missing", "uid", "version")
				} else {
					c.Get("key", "uid", "version")
				}
			}
		})
	}
}
