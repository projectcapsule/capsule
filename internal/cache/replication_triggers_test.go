// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type triggerValidationCounter struct{ calls int }

func (c *triggerValidationCounter) ValidateTriggerCondition(string) error { c.calls++; return nil }

func TestPreparedSubscriptionsValidateColdAndCurrentGenerations(t *testing.T) {
	prepared := NewPreparedReplicationTriggers()
	compiler := &triggerValidationCounter{}
	spec := &capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, MatchConditions: []admissionv1.MatchCondition{{Name: "changed", Expression: "object.data != oldObject.data"}}}}}
	if err := prepared.Validate("a", 1, spec, true, compiler); err != nil || compiler.calls != 1 {
		t.Fatalf("cold validation: calls=%d err=%v", compiler.calls, err)
	}
	if _, err := prepared.GetOrBuild("a", 1, 1, [32]byte{1}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Validate("a", 1, spec, true, compiler); err != nil || compiler.calls != 1 {
		t.Fatalf("current generation recompiled: calls=%d err=%v", compiler.calls, err)
	}
	for _, source := range []struct {
		uid        types.UID
		generation int64
	}{{"b", 1}, {"a", 2}} {
		if err := prepared.Validate(source.uid, source.generation, spec, true, compiler); err != nil {
			t.Fatal(err)
		}
	}
	if compiler.calls != 3 {
		t.Fatal("validation leaked across resource identity or generation")
	}
	cel, _ := NewCELCache()
	spec.Triggers[0].MatchConditions[0].Expression = "request.operation == 'UPDATE'"
	// A persisted spec edit always increments generation.
	if err := prepared.Validate("a", 3, spec, true, cel); err == nil {
		t.Fatal("invalid CEL accepted without admission")
	}
}

func emptyPrepared(tb testing.TB) *replication.PreparedTriggers {
	tb.Helper()
	value, err := replication.PrepareTriggers(&capsulev1beta2.TenantResourceCommonSpec{}, true, nil, nil)
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

func TestPreparedSubscriptionCacheReuseScopePruningAndBounds(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	artifact := emptyPrepared(t)
	builds := 0
	build := func() (*replication.PreparedTriggers, error) { builds++; return artifact, nil }
	fingerprint := [32]byte{1}
	if _, err := c.GetOrBuild("a", 1, 1, fingerprint, build); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetOrBuild("a", 2, 1, fingerprint, build); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatal("unrelated generation edit recompiled conditions")
	}
	if _, ok := c.Get("a", 1); ok {
		t.Fatal("stale generation matched")
	}
	if _, ok := c.Get("b", 2); ok {
		t.Fatal("source identity leaked")
	}
	if _, err := c.GetOrBuild("a", 3, 1, [32]byte{2}, build); err != nil {
		t.Fatal(err)
	}
	if builds != 2 {
		t.Fatal("policy update reused old projection")
	}
	c.PruneActive(map[types.UID]int64{})
	if _, ok := c.Get("a", 3); ok || c.conditions != 0 {
		t.Fatal("deleted projection retained")
	}
	if _, err := c.GetOrBuild("a", 1, MaxPreparedTriggerConditions, fingerprint, build); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetOrBuild("b", 1, 1, fingerprint, build); err == nil {
		t.Fatal("condition budget unbounded")
	}
	c.Delete("a")
	for i := range MaxPreparedReplications {
		if _, err := c.GetOrBuild(types.UID(fmt.Sprint(i)), 1, 0, fingerprint, build); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.GetOrBuild("overflow", 1, 0, fingerprint, build); err == nil {
		t.Fatal("source cache unbounded")
	}
}

func TestPreparedSubscriptionConcurrentPublication(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	artifact := emptyPrepared(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			value, err := c.GetOrBuild("a", 1, 1, [32]byte{1}, func() (*replication.PreparedTriggers, error) { return artifact, nil })
			if err != nil || value != artifact {
				t.Errorf("publication failed: %v", err)
			}
		})
	}
	wg.Wait()
	if c.conditions != 1 || len(c.entries) != 1 {
		t.Fatal("concurrent misses duplicated retained state")
	}
	started, release := make(chan struct{}), make(chan struct{})
	wg.Go(func() {
		_, err := c.GetOrBuild("a", 2, 1, [32]byte{2}, func() (*replication.PreparedTriggers, error) { close(started); <-release; return artifact, nil })
		if err == nil {
			t.Error("stale builder replaced newer generation")
		}
	})
	<-started
	if _, err := c.GetOrBuild("a", 3, 1, [32]byte{3}, func() (*replication.PreparedTriggers, error) { return artifact, nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if _, ok := c.Get("a", 3); !ok {
		t.Fatal("newer projection lost")
	}
}

func BenchmarkPreparedWatchSubscription(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			c := NewPreparedReplicationTriggers()
			artifact := emptyPrepared(b)
			build := func() (*replication.PreparedTriggers, error) { return artifact, nil }
			_, _ = c.GetOrBuild("a", 1, 0, [32]byte{}, build)
			b.ReportAllocs()
			if mode == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if _, ok := c.Get("a", 1); !ok {
							b.Fatal("cache miss")
						}
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c.Delete("a")
				}
				if _, err := c.GetOrBuild("a", 1, 0, [32]byte{}, build); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWatchSpecValidation(b *testing.B) {
	for _, prepared := range []bool{false, true} {
		b.Run(fmt.Sprintf("prepared=%t", prepared), func(b *testing.B) {
			compiler, _ := NewCELCache()
			cache := NewPreparedReplicationTriggers()
			spec := &capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, MatchConditions: []admissionv1.MatchCondition{{Name: "changed", Expression: "object.data != oldObject.data"}}}}}
			if err := cache.Validate("a", 1, spec, true, compiler); err != nil {
				b.Fatal(err)
			}
			if prepared {
				if _, err := cache.GetOrBuild("a", 1, 1, [32]byte{1}, func() (*replication.PreparedTriggers, error) {
					return replication.PrepareTriggers(spec, true, NewLabelSelectorCache(), compiler)
				}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := cache.Validate("a", 1, spec, true, compiler); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
