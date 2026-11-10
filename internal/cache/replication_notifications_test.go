// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestReplicationNotificationCoalescing(t *testing.T) {
	c := NewReplicationNotifications()
	now := time.Unix(100, 0)
	target := replication.Target{Name: "a", UID: "one"}
	for range 100 {
		if !c.Notify(target, 10*time.Second, now) {
			t.Fatal("unexpected rejection")
		}
	}
	if _, ok := c.Next(now); ok {
		t.Fatal("initial settling delay was skipped")
	}
	if got, ok := c.Next(now.Add(time.Second)); !ok || got != target {
		t.Fatal("notification missing")
	}
	// An event during I/O must remain pending.
	c.Notify(target, 10*time.Second, now.Add(time.Second))
	c.Complete(target, now.Add(time.Second), false)
	if _, ok := c.Next(now.Add(10 * time.Second)); ok {
		t.Fatal("cooldown was bypassed")
	}
	if _, ok := c.Next(now.Add(11 * time.Second)); !ok {
		t.Fatal("trailing notification lost")
	}
	c.Complete(target, now.Add(11*time.Second), false)
	c.Next(now.Add(22 * time.Second))
	if len(c.entries) != 0 {
		t.Fatal("idle entries did not expire")
	}
}

func TestReplicationNotificationConcurrencyBoundsAndRetry(t *testing.T) {
	c := NewReplicationNotifications()
	now := time.Unix(100, 0)
	target := replication.Target{Name: "a", UID: "one"}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { c.Notify(target, time.Second, now) })
	}
	wg.Wait()
	if len(c.entries) != 1 {
		t.Fatal("concurrent misses were not coalesced")
	}
	c.Next(now.Add(time.Second))
	c.Complete(target, now.Add(time.Second), true)
	if _, ok := c.Next(now.Add(1500 * time.Millisecond)); ok {
		t.Fatal("retry did not back off")
	}
	if _, ok := c.Next(now.Add(2 * time.Second)); !ok {
		t.Fatal("failed notification was lost")
	}
	for i := 1; i < MaxReplicationNotifications; i++ {
		if !c.Notify(replication.Target{Name: fmt.Sprint(i)}, time.Second, now) {
			t.Fatal("premature saturation")
		}
	}
	if c.Notify(replication.Target{Name: "overflow"}, time.Second, now) {
		t.Fatal("unbounded pending state")
	}
	if !c.Notify(target, time.Second, now) {
		t.Fatal("saturated queue rejected coalescing")
	}
	c.Prune(map[replication.Target]struct{}{target: {}})
	if len(c.entries) != 1 {
		t.Fatal("inactive incarnation entries survived pruning")
	}
	c.Prune(nil)
	if len(c.entries) != 0 {
		t.Fatal("deleted resources retain queue capacity")
	}
}

func BenchmarkReplicationNotifications(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		b.Run(fmt.Sprintf("parallel=%v", parallel), func(b *testing.B) {
			c := NewReplicationNotifications()
			now := time.Unix(100, 0)
			target := replication.Target{Name: "a", UID: "one"}
			b.ReportAllocs()
			if parallel {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if !c.Notify(target, time.Second, now) {
							b.Fatal("rejected")
						}
					}
				})
			} else {
				for b.Loop() {
					if !c.Notify(target, time.Second, now) {
						b.Fatal("rejected")
					}
				}
			}
		})
	}
}

func TestNotificationIntervalDecreasePreservesSettlingAndRetry(t *testing.T) {
	for _, state := range []string{"settling", "cooldown", "retry", "deferred", "during-defer"} {
		t.Run(state, func(t *testing.T) {
			c := NewReplicationNotifications()
			now := time.Unix(100, 0)
			target := replication.Target{Name: "a", UID: "one"}
			c.Notify(target, 24*time.Hour, now)
			if state == "settling" {
				c.Notify(target, time.Second, now)
				if _, ok := c.Next(now); ok {
					t.Fatal("policy edit bypassed settling")
				}
				return
			}
			c.Next(now.Add(time.Second))
			if state == "deferred" || state == "during-defer" {
				if state == "during-defer" {
					c.Notify(target, time.Second, now.Add(time.Second))
				}
				c.Defer(target, now.Add(time.Second), 24*time.Hour)
			} else {
				c.Complete(target, now.Add(time.Second), state == "retry")
			}
			c.Notify(target, time.Second, now.Add(1500*time.Millisecond))
			if state == "retry" {
				if _, ok := c.Next(now.Add(1500 * time.Millisecond)); ok {
					t.Fatal("policy edit bypassed retry backoff")
				}
			}
			if _, ok := c.Next(now.Add(2 * time.Second)); !ok {
				t.Fatal("shorter policy retained day-long deadline")
			}
		})
	}
}
