// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHealthProgramsReuseResetAndIsolation(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	expression := `status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')`
	first, err := c.GetOrCompileHealthCondition(expression)
	require.NoError(t, err)
	again, err := c.GetOrCompileHealthCondition(expression)
	require.NoError(t, err)
	require.Same(t, first, again)
	_, err = c.GetOrCompileTriggerCondition(expression)
	require.Error(t, err, "health bindings must not leak into watch CEL")
	for _, value := range []string{"True", "False"} {
		result, err := first.EvaluateBooleanWithVariables(t.Context(), map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": value}}}})
		require.NoError(t, err)
		require.Equal(t, value == "True", result)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			program, err := c.GetOrCompileHealthCondition(expression)
			if err != nil {
				t.Error(err)
				return
			}
			if program != first {
				t.Error("program was not reused")
			}
		})
	}
	wg.Wait()
	c.Reset()
	newer, err := c.GetOrCompileHealthCondition(expression)
	require.NoError(t, err)
	require.NotSame(t, first, newer)
	ok, err := first.EvaluateBooleanWithVariables(t.Context(), map[string]any{"status": map[string]any{"conditions": []any{}}})
	require.NoError(t, err)
	require.False(t, ok)
	_, err = c.GetOrCompileHealthCondition(`request.userInfo.username == 'alice'`)
	require.Error(t, err)
}

func TestHealthConcurrentCacheMisses(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			program, err := c.GetOrCompileHealthCondition(`object.data.value == 'tenant-a'`)
			if err != nil {
				t.Error(err)
				return
			}
			value, err := program.EvaluateBoolean(t.Context(), unstructured.Unstructured{Object: map[string]any{"data": map[string]any{"value": "tenant-b"}}})
			if err != nil || value {
				t.Errorf("unexpected evaluation: %t %v", value, err)
			}
		})
	}
	wg.Wait()
}

func BenchmarkHealthCELCache(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			c, _ := NewCELCache()
			expression := `has(object.status) && object.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')`
			if _, err := c.GetOrCompileHealthCondition(expression); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if _, err := c.GetOrCompileHealthCondition(expression); err != nil {
							b.Error(err)
						}
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c.Reset()
				}
				if _, err := c.GetOrCompileHealthCondition(expression); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestHealthCacheIsBoundedAndPreservesLifecyclePrograms(t *testing.T) {
	c, err := NewCELCache()
	require.NoError(t, err)
	first, err := c.GetOrCompileHealthCondition("true")
	require.NoError(t, err)
	c.ResetQuotaExpressions()
	again, err := c.GetOrCompileHealthCondition("true")
	require.NoError(t, err)
	require.Same(t, first, again)
	require.Equal(t, 1, c.DeleteMany("true"))
	newer, err := c.GetOrCompileHealthCondition("true")
	require.NoError(t, err)
	require.NotSame(t, first, newer)
	for i := range maxResourceConditions + 10 {
		_, err := c.GetOrCompileHealthCondition(fmt.Sprintf("object.value == %d", i))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, c.Stats(), maxResourceConditions)
	ok, err := first.EvaluateBooleanWithVariables(t.Context(), map[string]any{})
	require.NoError(t, err)
	require.True(t, ok)
}
