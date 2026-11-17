// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestWatchConditionsReuseResetAndCost(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.GetOrCompileTriggerCondition("object.metadata.name == 'a'")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			other, err := c.GetOrCompileTriggerCondition("object.metadata.name == 'a'")
			if err != nil || other != first {
				t.Errorf("warm artifact not reused: %v", err)
			}
		})
	}
	wg.Wait()
	for i := range maxTriggerConditions + 1 {
		if _, err := c.GetOrCompileTriggerCondition(fmt.Sprintf("object.metadata.name != '%d'", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.triggerConditions) > maxTriggerConditions {
		t.Fatal("unbounded program cache")
	}
	c.Reset()
	if len(c.triggerConditions) != 0 {
		t.Fatal("reset retained programs")
	}
	// Retired immutable programs remain usable by active prepared subscriptions.
	if matched, err := first.EvaluateBooleanWithVariables(t.Context(), map[string]any{"object": map[string]any{"metadata": map[string]any{"name": "a"}}}); !matched || err != nil {
		t.Fatal(err)
	}
	expensive, err := c.GetOrCompileTriggerCondition("object.items.all(x, object.items.all(y, x == y || x != y))")
	if err != nil {
		t.Fatal(err)
	}
	items := make([]any, 500)
	for i := range items {
		items[i] = int64(i)
	}
	if _, err := expensive.EvaluateBooleanWithVariables(context.Background(), map[string]any{"object": map[string]any{"items": items}}); err == nil {
		t.Fatal("watch condition had no cost limit")
	}
}

func BenchmarkWatchConditionCompilation(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			c, err := NewCELCache()
			if err != nil {
				b.Fatal(err)
			}
			expression := "operation != 'UPDATE' || object.data != oldObject.data"
			if _, err := c.GetOrCompileTriggerCondition(expression); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			if mode == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if _, err := c.GetOrCompileTriggerCondition(expression); err != nil {
							b.Fatal(err)
						}
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c.Reset()
				}
				if _, err := c.GetOrCompileTriggerCondition(expression); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
