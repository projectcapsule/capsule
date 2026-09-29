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
	resourceCondition, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions)
	if err != nil {
		t.Fatal(err)
	}
	if resourceCondition == first || resourceCondition == quota || c.Stats() != 3 {
		t.Fatal("resource, quota, and admission environments were conflated")
	}
	if allowed, err := first.EvaluateCondition(t.Context(), nil, nil); err != nil || !allowed {
		t.Fatalf("EvaluateCondition() = %v, %v, want true", allowed, err)
	}
	if allowed, err := resourceCondition.EvaluateBooleanWithVariables(t.Context(), nil); err != nil || !allowed {
		t.Fatalf("EvaluateBooleanWithVariables() = %v, %v, want true", allowed, err)
	}
	c.PruneConditions(map[string]struct{}{expression: {}})
	again, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
	if err != nil || again != first {
		t.Fatal("retained condition was recompiled")
	}
	c.PruneConditions(nil)
	if c.Stats() != 2 {
		t.Fatal("quota or resource condition entry pruned")
	}
	resourceAgain, err := c.GetOrCompileResourceCondition(expression, environment.StoredExpressions)
	if err != nil || resourceAgain != resourceCondition {
		t.Fatal("resource condition was recompiled after pruning admission conditions")
	}
	rebuilt, err := c.GetOrCompileCondition(expression, environment.StoredExpressions)
	if err != nil || rebuilt == first {
		t.Fatal("stale program not replaced")
	}
	c.ResetQuotaExpressions()
	if c.Stats() != 2 {
		t.Fatal("quota reset removed admission or resource conditions")
	}
	again, err = c.GetOrCompileCondition(expression, environment.StoredExpressions)
	if err != nil || again != rebuilt {
		t.Fatal("admission condition was recompiled after resetting quota expressions")
	}
	resourceAgain, err = c.GetOrCompileResourceCondition(expression, environment.StoredExpressions)
	if err != nil || resourceAgain != resourceCondition {
		t.Fatal("resource condition was recompiled after resetting quota expressions")
	}
}
