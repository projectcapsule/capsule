// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
	admissionv1 "k8s.io/api/admissionregistration/v1"
)

func TestNativeManagedConditionOldAndNew(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	hook := replication.Webhook(&capsulev1beta2.TenantResourceTrigger{}, "", admissionv1.WebhookClientConfig{})
	var expression string
	for _, condition := range hook.MatchConditions {
		if condition.Name == "capsule-trigger-unmanaged" {
			expression = condition.Expression
		}
	}
	if err := c.ValidateTriggerCondition(expression); err != nil {
		t.Fatal(err)
	}
	program := c.triggerConditions[expression].Program
	plain := map[string]any{"metadata": map[string]any{"name": "source"}}
	managed := map[string]any{"metadata": map[string]any{"labels": map[string]any{"projectcapsule.dev/managed-by": "replications"}}}
	for _, tc := range []struct {
		name     string
		new, old any
		want     bool
	}{{"create", plain, nil, true}, {"delete", nil, plain, true}, {"managed-create", managed, nil, false}, {"managed-delete", nil, managed, false}, {"marker-removed", plain, managed, false}, {"marker-added", managed, plain, false}, {"update", plain, plain, true}} {
		t.Run(tc.name, func(t *testing.T) {
			value, _, err := program.ContextEval(t.Context(), map[string]any{"object": tc.new, "oldObject": tc.old})
			if err != nil || value.Value() != tc.want {
				t.Fatalf("eligibility=%v error=%v want=%v", value, err, tc.want)
			}
		})
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := c.ValidateTriggerCondition(expression); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(c.triggerConditions) != 1 {
		t.Fatal("concurrent compilation did not reuse entry")
	}
	c.Reset()
	if len(c.triggerConditions) != 0 {
		t.Fatal("reset retained native conditions")
	}
	var cold sync.WaitGroup
	for range 32 {
		cold.Go(func() {
			if err := c.ValidateTriggerCondition("request.namespace == 'shared'"); err != nil {
				t.Error(err)
			}
		})
	}
	cold.Wait()
	if len(c.triggerConditions) != 1 {
		t.Fatal("concurrent native misses published duplicate entries")
	}
}

func BenchmarkNativeTriggerConditions(b *testing.B) {
	for _, state := range []string{"warm", "cold", "invalidated", "concurrent"} {
		b.Run(state, func(b *testing.B) {
			c, err := NewCELCache()
			if err != nil {
				b.Fatal(err)
			}
			expression := "request.operation == 'UPDATE'"
			if err := c.ValidateTriggerCondition(expression); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			if state == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if err := c.ValidateTriggerCondition(expression); err != nil {
							b.Error(err)
						}
					}
				})
				return
			}
			index := 0
			for b.Loop() {
				expr := expression
				if state == "cold" {
					expr = fmt.Sprintf("request.name == 'name-%d'", index)
					index++
				}
				if state == "invalidated" {
					c.Reset()
				}
				if err := c.ValidateTriggerCondition(expr); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
