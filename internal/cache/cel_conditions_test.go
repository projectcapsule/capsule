// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"
	"testing"

	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

func TestConcurrentConditionCacheReusesProgramsAndSeparatesEnvironments(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	const expression = "true"
	var wg sync.WaitGroup
	results := make(chan *celruntime.CompiledExpression, 32)
	for range 32 {
		wg.Go(func() {
			compiled, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
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
	for compiled := range results {
		if first == nil {
			first = compiled
		}
		if compiled != first {
			t.Fatal("concurrent misses returned different programs")
		}
	}
	quota, err := c.GetOrCompileBoolean(expression, environment.StoredExpressions)
	if err != nil {
		t.Fatal(err)
	}
	if quota == first || c.Stats() != 2 {
		t.Fatal("quota and admission environments were conflated")
	}
	c.PruneConditions(map[string]struct{}{expression: {}})
	again, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
	if err != nil || again != first {
		t.Fatal("retained condition was recompiled")
	}
	c.PruneConditions(nil)
	if c.Stats() != 1 {
		t.Fatal("quota entry pruned")
	}
	rebuilt, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
	if err != nil || rebuilt == first {
		t.Fatal("stale program not replaced")
	}
}
