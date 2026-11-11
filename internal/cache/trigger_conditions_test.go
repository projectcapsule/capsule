// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func TestNativeManagedConditionOldAndNew(t *testing.T) {
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	hook := replication.Webhook(&capsulev1beta2.TenantResourceTrigger{}, "", admissionv1.WebhookClientConfig{}, testDeliveryKey(t))
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
	for _, tenantName := range []string{"controller", "resources", "replications"} {
		obj := map[string]any{"metadata": map[string]any{"labels": map[string]any{"capsule.clastix.io/managed-by": tenantName, "projectcapsule.dev/tenant": tenantName}}}
		value, _, err := program.ContextEval(t.Context(), map[string]any{"object": obj, "oldObject": obj})
		if err != nil || value.Value() != true {
			t.Fatalf("tenant %s incorrectly excluded: %v %v", tenantName, value, err)
		}
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

func TestNativeBookkeepingExclusionPreservesUserUpdates(t *testing.T) {
	t.Setenv("SERVICE_ACCOUNT", "capsule")
	t.Setenv("NAMESPACE", "capsule-system")
	c, err := NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	hook := replication.Webhook(&capsulev1beta2.TenantResourceTrigger{}, "", admissionv1.WebhookClientConfig{}, testDeliveryKey(t))
	var expression string
	for _, condition := range hook.MatchConditions {
		if condition.Name == "capsule-trigger-bookkeeping" {
			expression = condition.Expression
		}
	}
	if expression == "" {
		t.Fatal("missing native bookkeeping gate")
	}
	if err := c.ValidateTriggerCondition(expression); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user, group, resource, operation string
		want                             bool
	}{
		{"system:serviceaccount:capsule-system:capsule", capsulev1beta2.GroupVersion.Group, "tenantresources", "UPDATE", false},
		{"system:serviceaccount:capsule-system:capsule", capsulev1beta2.GroupVersion.Group, "globaltenantresources", "UPDATE", false},
		{"alice", capsulev1beta2.GroupVersion.Group, "tenantresources", "UPDATE", true},
		{"system:serviceaccount:capsule-system:capsule", "", "secrets", "UPDATE", true},
		{"system:serviceaccount:capsule-system:capsule", capsulev1beta2.GroupVersion.Group, "tenantresources", "CREATE", true},
	} {
		request := map[string]any{"operation": tc.operation, "resource": map[string]any{"group": tc.group, "resource": tc.resource}, "userInfo": map[string]any{"username": tc.user}}
		value, _, err := c.triggerConditions[expression].Program.ContextEval(t.Context(), map[string]any{"request": request})
		if err != nil || value.Value() != tc.want {
			t.Fatalf("%+v: result=%v err=%v", tc, value, err)
		}
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

func testDeliveryKey(tb testing.TB) *replication.DeliveryKey {
	tb.Helper()
	key, err := replication.NewDeliveryKey(make([]byte, 32))
	if err != nil {
		tb.Fatal(err)
	}
	return key
}
