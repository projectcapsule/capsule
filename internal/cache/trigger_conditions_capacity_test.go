// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync/atomic"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	admissioncel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/cel/environment"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type countingTriggerCompiler struct {
	admissioncel.Compiler
	calls atomic.Int64
}

func (c *countingTriggerCompiler) CompileCELExpression(e admissioncel.ExpressionAccessor, o admissioncel.OptionalVariableDeclarations, m environment.Type) admissioncel.CompilationResult {
	c.calls.Add(1)
	return c.Compiler.CompileCELExpression(e, o, m)
}

func boundaryTriggers(tenant int) *capsulev1beta2.TenantResourceCommonSpec {
	spec := &capsulev1beta2.TenantResourceCommonSpec{}
	for group := range 32 {
		trigger := capsulev1beta2.TenantResourceTrigger{Name: fmt.Sprintf("group-%d", group), Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}
		for condition := range 60 {
			trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("condition-%d", condition), Expression: fmt.Sprintf("request.name != 'tenant-%d-group-%d-condition-%d'", tenant, group, condition)})
		}
		spec.Triggers = append(spec.Triggers, trigger)
	}
	return spec
}

func TestNativeTriggerCacheMaximumSpecifications(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateTriggerCondition("true"); err != nil {
		t.Fatal(err)
	}
	counter := &countingTriggerCompiler{Compiler: c.triggerCompiler}
	c.triggerCompiler = counter
	specs := []*capsulev1beta2.TenantResourceCommonSpec{boundaryTriggers(0), boundaryTriggers(1), boundaryTriggers(2), boundaryTriggers(3)}
	for _, spec := range specs {
		if err := replication.ValidateTriggers(spec, true, c); err != nil {
			t.Fatal(err)
		}
	}
	calls := counter.calls.Load()
	for range 2 {
		for _, spec := range specs {
			if err := replication.ValidateTriggers(spec, true, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 7680 || counter.calls.Load() != calls {
		t.Fatalf("warm validation recompiled conditions: initial=%d final=%d", calls, counter.calls.Load())
	}
	// Overflow evicts individual oldest entries, preserving the remaining set.
	for i := range maxTriggerConditions - 7681 + 1 {
		if err := c.ValidateTriggerCondition(fmt.Sprintf("request.name != 'overflow-%d'", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.triggerConditions) != maxTriggerConditions {
		t.Fatalf("cache size=%d", len(c.triggerConditions))
	}
	if _, ok := c.triggerConditions["true"]; ok {
		t.Fatal("oldest entry was not evicted")
	}
	calls = counter.calls.Load()
	if err := c.ValidateTriggerCondition(specs[1].Triggers[31].MatchConditions[59].Expression); err != nil || counter.calls.Load() != calls {
		t.Fatal("overflow cleared live cache entries")
	}
	c.Reset()
	if len(c.triggerConditions) != 0 || len(c.triggerOrder) != 0 || c.triggerNext != 0 {
		t.Fatal("reset retained validation entries")
	}
}

func BenchmarkTriggerValidationBoundary(b *testing.B) {
	for _, count := range []int{1, 4} {
		for _, state := range []string{"warm", "cold", "concurrent"} {
			b.Run(fmt.Sprintf("tenants=%d/%s", count, state), func(b *testing.B) {
				c, err := NewCELCache()
				if err != nil {
					b.Fatal(err)
				}
				specs := make([]*capsulev1beta2.TenantResourceCommonSpec, count)
				for i := range specs {
					specs[i] = boundaryTriggers(i)
					if err := replication.ValidateTriggers(specs[i], true, c); err != nil {
						b.Fatal(err)
					}
				}
				counter := &countingTriggerCompiler{Compiler: c.triggerCompiler}
				c.triggerCompiler = counter
				validate := func() {
					for _, spec := range specs {
						if err := replication.ValidateTriggers(spec, true, c); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.ReportAllocs()
				defer func() { b.ReportMetric(float64(counter.calls.Load())/float64(b.N), "compilations/op") }()
				if state == "concurrent" {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							validate()
						}
					})
					return
				}
				for b.Loop() {
					if state == "cold" {
						c.Reset()
					}
					validate()
				}
			})
		}
	}
}
