// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func fullPreparationCache(tb testing.TB, limit string) *PreparedReplicationTriggers {
	tb.Helper()
	c := NewPreparedReplicationTriggers()
	artifact := emptyPrepared(tb)
	count, conditions := 1, MaxPreparedTriggerConditions
	if limit == "sources" {
		count, conditions = MaxPreparedReplications, 0
	}
	for index := range count {
		if _, err := c.GetOrBuild(types.UID(fmt.Sprintf("capacity-%d", index)), 1, conditions, [32]byte{1}, func() (*replication.PreparedTriggers, error) { return artifact, nil }); err != nil {
			tb.Fatal(err)
		}
	}
	return c
}

func TestSuccessfulValidationSurvivesPreparationCapacity(t *testing.T) {
	for _, limit := range []string{"sources", "conditions"} {
		t.Run(limit, func(t *testing.T) {
			c := fullPreparationCache(t, limit)
			compiler, _ := NewCELCache()
			calls := 0
			counted := triggerValidationFunc(func(expression string) error { calls++; return compiler.ValidateTriggerCondition(expression) })
			spec := validationSpec(1920)
			for range 5 {
				if err := c.Validate("omitted", 1, spec, true, counted); err != nil {
					t.Fatal(err)
				}
				_, err := c.GetOrBuild("omitted", 1, 1920, [32]byte{2}, func() (*replication.PreparedTriggers, error) {
					t.Fatal("exhausted capacity started preparation")
					return nil, nil
				})
				if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
					t.Fatalf("capacity failure: %v", err)
				}
			}
			if calls != 1920 {
				t.Fatalf("unchanged valid source recompiled under %s capacity: %d calls", limit, calls)
			}
			c.Delete("capacity-0")
			if _, err := c.GetOrBuild("omitted", 1, 1920, [32]byte{2}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
				t.Fatalf("temporary capacity error blocked recovery: %v", err)
			}
			if err := c.Validate("omitted", 1, spec, true, counted); err != nil || calls != 1920 {
				t.Fatalf("prepared source lost validation: calls=%d err=%v", calls, err)
			}
			spec.Triggers[31].MatchConditions[59].Expression = "request.operation == 'UPDATE'"
			if err := c.Validate("omitted", 2, spec, true, counted); err == nil {
				t.Fatal("successful validation leaked into edited generation")
			}
			if _, ok := c.Get("omitted", 1); ok {
				t.Fatal("invalid edit retained the old subscription")
			}
		})
	}
}

func BenchmarkValidTriggerValidationAtCapacity(b *testing.B) {
	for _, limit := range []string{"sources", "conditions"} {
		for _, mode := range []string{"cold", "warm", "parallel"} {
			b.Run(limit+"/"+mode, func(b *testing.B) {
				c := fullPreparationCache(b, limit)
				compiler, _ := NewCELCache()
				var calls atomic.Int64
				counted := triggerValidationFunc(func(expression string) error { calls.Add(1); return compiler.ValidateTriggerCondition(expression) })
				spec := validationSpec(1920)
				if err := c.Validate("omitted", 1, spec, true, counted); err != nil {
					b.Fatal(err)
				}
				calls.Store(0)
				b.ReportAllocs()
				if mode == "parallel" {
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if err := c.Validate("omitted", 1, spec, true, counted); err != nil {
								b.Error(err)
							}
						}
					})
				} else {
					for b.Loop() {
						if mode == "cold" {
							c.Delete("omitted")
						}
						if err := c.Validate("omitted", 1, spec, true, counted); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.ReportMetric(float64(calls.Load())/float64(b.N), "CEL-validation/op")
			})
		}
	}
}

func TestSuccessfulValidationRetentionAndConcurrentPublication(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	spec := validationSpec(1)
	var calls atomic.Int64
	compiler := triggerValidationFunc(func(string) error { calls.Add(1); return nil })
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			for range 20 {
				if err := c.Validate("omitted", 1, spec, true, compiler); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if calls.Load() > 32 || len(c.validations) != 1 {
		t.Fatalf("successful validation was not reused: calls=%d entries=%d", calls.Load(), len(c.validations))
	}
	for index := range MaxPreparedReplications + 10 {
		_ = c.Validate(types.UID(fmt.Sprint(index)), 1, spec, true, compiler)
	}
	if len(c.validations) != MaxPreparedReplications {
		t.Fatal("successful validation retention is unbounded")
	}
	c.PruneActive(map[types.UID]int64{"omitted": 2})
	if len(c.validations) != 0 {
		t.Fatal("deleted and changed source generations retained old validation")
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrBuild("omitted", 1, 0, [32]byte{1}, func() (*replication.PreparedTriggers, error) {
			close(started)
			<-release
			return emptyPrepared(t), nil
		})
		done <- err
	}()
	<-started
	if err := c.Validate("omitted", 2, spec, true, compiler); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late builder replaced newer successful validation")
	}
	if _, ok := c.Get("omitted", 1); ok {
		t.Fatal("obsolete generation remained eligible")
	}
	if _, err := c.GetOrBuild("omitted", 2, 0, [32]byte{2}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.validationResult("omitted", 2); ok {
		t.Fatal("prepared source retained redundant validation state")
	}
}

func TestNewerValidationSurvivesStaleInformerPruning(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			c := NewPreparedReplicationTriggers()
			if _, err := c.GetOrBuild("source", 1, 0, [32]byte{1}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
				t.Fatal(err)
			}
			compiler := triggerValidationFunc(func(string) error {
				if invalid {
					return fmt.Errorf("invalid expression")
				}
				return nil
			})
			_ = c.Validate("source", 2, validationSpec(1), true, compiler)
			c.PruneActive(map[types.UID]int64{"source": 1})
			if err, ok := c.validationResult("source", 2); !ok || (err != nil) != invalid {
				t.Fatalf("stale informer discarded newer validation: cached=%t err=%v", ok, err)
			}
			if _, ok := c.Get("source", 1); ok {
				t.Fatal("stale subscription became eligible after pruning")
			}
			c.Delete("source")
			if len(c.validations) != 0 {
				t.Fatal("source deletion retained validation state")
			}
		})
	}
}
