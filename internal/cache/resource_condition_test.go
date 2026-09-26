// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

func TestResourceConditionCache(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	expression := `object == null || now >= timestamp(object.metadata.annotations['rotated-at']) + duration('5m')`
	compiled, err := c.GetOrCompileResourceCondition(expression, environment.NewExpressions)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			got, err := c.GetOrCompileResourceCondition(expression, environment.NewExpressions)
			if err != nil || got != compiled {
				t.Error("compiled condition was not reused")
			}
		})
	}
	wg.Wait()
	if _, err := c.GetOrCompileBoolean(expression, environment.NewExpressions); err == nil {
		t.Fatal("resource variables leaked into the standard CEL environment")
	}
	rotated := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	object := map[string]any{"metadata": map[string]any{"annotations": map[string]any{"rotated-at": rotated.Format(time.RFC3339)}}}
	for _, tc := range []struct {
		name            string
		object          any
		now             time.Time
		want, wantError bool
	}{
		{name: "first creation", now: rotated, want: true},
		{name: "before five minutes", object: object, now: rotated.Add(5*time.Minute - time.Nanosecond)},
		{name: "exactly five minutes", object: object, now: rotated.Add(5 * time.Minute), want: true},
		{name: "after five minutes", object: object, now: rotated.Add(6 * time.Minute), want: true},
		{name: "bad timestamp", object: map[string]any{"metadata": map[string]any{"annotations": map[string]any{"rotated-at": "bad"}}}, now: rotated, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compiled.EvaluateBooleanWithVariables(t.Context(), map[string]any{"object": tc.object, "now": tc.now})
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("got %v, error=%v", got, err)
			}
		})
	}
	if n := c.DeleteMany(expression); n != 1 {
		t.Fatalf("deleted %d entries", n)
	}
	next, err := c.GetOrCompileResourceCondition(expression, environment.NewExpressions)
	if err != nil || next == compiled {
		t.Fatal("invalidation did not rebuild condition")
	}
	for i := range maxResourceConditions + 2 {
		if _, err := c.GetOrCompileResourceCondition(fmt.Sprintf("object == null || %d == 0", i), environment.StoredExpressions); err != nil {
			t.Fatal(err)
		}
	}
	if c.resourceConditions > maxResourceConditions || c.Stats() > maxResourceConditions {
		t.Fatal("unbounded condition cache")
	}
}

func TestResourceConditionConcurrentMiss(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan *celruntime.CompiledExpression, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			compiled, err := c.GetOrCompileResourceCondition(`object == null`, environment.StoredExpressions)
			if err != nil {
				t.Error(err)
				return
			}
			results <- compiled
		})
	}
	wg.Wait()
	close(results)
	var first *celruntime.CompiledExpression
	for result := range results {
		if first != nil && first != result {
			t.Fatal("concurrent miss published duplicate programs")
		}
		first = result
	}
}

func TestCELCacheReset(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	for _, mode := range []environment.Type{environment.NewExpressions, environment.StoredExpressions} {
		_, err = c.GetOrCompileBoolean("true", mode)
		require.NoError(t, err)
		_, err = c.GetOrCompileQuantity(`quantity('1')`, mode)
		require.NoError(t, err)
		_, err = c.GetOrCompileResourceCondition("object == null", mode)
		require.NoError(t, err)
	}
	require.Equal(t, 6, c.Stats())
	old, err := c.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
	require.NoError(t, err)
	c.Reset()
	require.Zero(t, c.Stats())
	// In-flight evaluations retain immutable programs after the cache retires them.
	allowed, err := old.EvaluateBooleanWithVariables(t.Context(), map[string]any{"object": nil})
	require.NoError(t, err)
	require.True(t, allowed)
	next, err := c.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
	require.NoError(t, err)
	require.NotSame(t, old, next)
	for i := 1; i < maxResourceConditions; i++ {
		_, err = c.GetOrCompileResourceCondition(fmt.Sprintf("object == null || %d == 0", i), environment.StoredExpressions)
		require.NoError(t, err)
	}
	require.Equal(t, maxResourceConditions, c.Stats(), "reset must also clear resource-condition accounting")
	c.Reset()
	c.Reset()
	require.Zero(t, c.Stats())
	(*CELCache)(nil).Reset()
}

func TestCELCacheResetQuotaExpressions(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	for _, mode := range []environment.Type{environment.NewExpressions, environment.StoredExpressions} {
		// Identical text must not confuse quota and resource-condition entries.
		quota, err := c.GetOrCompileBoolean("true", mode)
		require.NoError(t, err)
		_, err = c.GetOrCompileQuantity(`quantity('1')`, mode)
		require.NoError(t, err)
		condition, err := c.GetOrCompileResourceCondition("true", mode)
		require.NoError(t, err)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				c.ResetQuotaExpressions()
				reused, err := c.GetOrCompileResourceCondition("true", mode)
				if err != nil || reused != condition {
					t.Errorf("quota reset evicted a resource condition: %v", err)
				}
			})
		}
		wg.Wait()
		next, err := c.GetOrCompileBoolean("true", mode)
		require.NoError(t, err)
		require.NotSame(t, quota, next)
		allowed, err := quota.EvaluateBooleanWithVariables(t.Context(), nil)
		require.NoError(t, err)
		require.True(t, allowed, "in-flight quota evaluation must remain valid")
	}
	c.ResetQuotaExpressions()
	require.Equal(t, 2, c.Stats())
	for i := range maxResourceConditions {
		_, err = c.GetOrCompileResourceCondition(fmt.Sprintf("object == null || %d == 0", i), environment.StoredExpressions)
		require.NoError(t, err)
	}
	require.Equal(t, maxResourceConditions, c.Stats(), "quota resets must preserve condition eviction accounting")
	c.Reset()
	c.ResetQuotaExpressions()
	require.Zero(t, c.Stats())
	(*CELCache)(nil).ResetQuotaExpressions()
}

func TestCELCacheConcurrentReset(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for range 20 {
				compiled, err := c.GetOrCompileResourceCondition(`object.metadata.namespace == 'tenant-a'`, environment.StoredExpressions)
				if err != nil {
					t.Error(err)
					return
				}
				c.Reset()
				namespace := "tenant-a"
				if worker%2 != 0 {
					namespace = "tenant-b"
				}
				allowed, err := compiled.EvaluateBooleanWithVariables(t.Context(), map[string]any{
					"object": map[string]any{"metadata": map[string]any{"namespace": namespace}},
				})
				if err != nil || allowed != (worker%2 == 0) {
					t.Errorf("namespace %s: allowed=%v, error=%v", namespace, allowed, err)
				}
			}
		})
	}
	wg.Wait()
	c.Reset()
	require.Zero(t, c.Stats())
}

func BenchmarkResourceCondition(b *testing.B) {
	c, err := NewCELCache()
	if err != nil {
		b.Fatal(err)
	}
	expression := `object == null || now >= timestamp(object.metadata.annotations['rotated-at']) + duration('5m')`
	compiled, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("warm-cache", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cold-cache", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			c.DeleteMany(expression)
			if _, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("parallel-cache", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
					b.Error(err)
				}
			}
		})
	})
	for name, reset := range map[string]func(){"reset-cache": c.Reset, "quota-reset-cache": c.ResetQuotaExpressions} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				reset()
				if _, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	for name, reset := range map[string]func(){"parallel-reset": c.Reset, "parallel-quota-reset": c.ResetQuotaExpressions} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				calls := 0
				for pb.Next() {
					if calls%128 == 0 {
						reset()
					}
					calls++
					if _, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
						b.Error(err)
					}
				}
			})
		})
	}

	b.Run("evaluate", func(b *testing.B) {
		variables := map[string]any{"object": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"rotated-at": "2026-09-21T10:00:00Z"}}}, "now": time.Date(2026, 9, 21, 10, 5, 0, 0, time.UTC)}
		b.ReportAllocs()
		for b.Loop() {
			allowed, err := compiled.EvaluateBooleanWithVariables(b.Context(), variables)
			if err != nil || !allowed {
				b.Fatalf("allowed=%v, error=%v", allowed, err)
			}
		}
	})
}
