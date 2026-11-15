// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type triggerValidationFunc func(string) error

func (f triggerValidationFunc) ValidateTriggerCondition(expression string) error {
	return f(expression)
}

func validationSpec(count int) *capsulev1beta2.TenantResourceCommonSpec {
	spec := &capsulev1beta2.TenantResourceCommonSpec{}
	for index := range count {
		if index%60 == 0 {
			spec.Triggers = append(spec.Triggers, capsulev1beta2.TenantResourceTrigger{Name: fmt.Sprintf("events-%d", index/60), Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}})
		}
		trigger := &spec.Triggers[len(spec.Triggers)-1]
		trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("condition-%d", index), Expression: fmt.Sprintf("object.metadata.name != 'value-%d'", index)})
	}
	return spec
}

func TestInvalidValidationDoesNotThrashTheCompileCache(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	compiler, _ := NewCELCache()
	var calls atomic.Int64
	counted := triggerValidationFunc(func(expression string) error {
		calls.Add(1)
		return compiler.ValidateTriggerCondition(expression)
	})
	spec := validationSpec(1920)
	spec.Triggers[31].MatchConditions[59].Expression = "request.operation == 'UPDATE'"
	if err := c.Validate("a", 1, spec, true, counted); err == nil {
		t.Fatal("invalid condition accepted")
	}
	for range 20 {
		if err := c.Validate("a", 1, spec, true, counted); err == nil {
			t.Fatal("cached invalid generation accepted")
		}
	}
	if calls.Load() != 1920 {
		t.Fatalf("unchanged failure recompiled: %d calls", calls.Load())
	}
	if _, err := c.GetOrBuild("a", 1, 1920, [32]byte{}, func() (*replication.PreparedTriggers, error) {
		t.Fatal("watch planning retried the same invalid generation")
		return nil, nil
	}); err == nil {
		t.Fatal("planning forgot validation failure")
	}
	c.PruneActive(map[types.UID]int64{"a": 2})
	if len(c.validations) != 0 {
		t.Fatal("generation update retained old failure")
	}
	spec.Triggers[31].MatchConditions[59].Expression = "true"
	if err := c.Validate("a", 2, spec, true, counted); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3840 {
		t.Fatal("new generation did not validate independently")
	}
}

func TestValidationFailurePublicationIsBoundedAndGenerationSafe(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	failure := errors.New("invalid condition")
	compiler := triggerValidationFunc(func(string) error { return failure })
	spec := validationSpec(1)
	if _, err := c.GetOrBuild("a", 1, 1, [32]byte{}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate("a", 2, spec, true, compiler); err == nil || c.conditions != 0 {
		t.Fatal("invalid edit retained its old programs")
	}
	c.Delete("a")
	if len(c.validations) != 0 {
		t.Fatal("deleted source retained a validation failure")
	}
	for index := range MaxPreparedReplications + 10 {
		_ = c.Validate(types.UID(fmt.Sprint(index)), 1, spec, true, compiler)
	}
	if len(c.validations) != MaxPreparedReplications {
		t.Fatal("validation failure retention is unbounded")
	}
	c.PruneActive(map[types.UID]int64{})
	if len(c.validations) != 0 {
		t.Fatal("deleted sources survived pruning")
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.Validate("a", 1, spec, true, triggerValidationFunc(func(string) error { close(started); <-release; return failure }))
	}()
	<-started
	if _, err := c.GetOrBuild("a", 2, 0, [32]byte{}, func() (*replication.PreparedTriggers, error) { return emptyPrepared(t), nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("old invalid input unexpectedly validated")
	}
	if _, ok := c.Get("a", 2); !ok || len(c.validations) != 0 {
		t.Fatal("late failed validation poisoned a newer generation")
	}
}

func BenchmarkFailedTriggerValidation(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "parallel"} {
		b.Run(mode, func(b *testing.B) {
			c := NewPreparedReplicationTriggers()
			spec := validationSpec(1)
			failure := errors.New("invalid expression")
			compiler := triggerValidationFunc(func(string) error { return failure })
			_ = c.Validate("a", 1, spec, true, compiler)
			validate := func() {
				if err := c.Validate("a", 1, spec, true, compiler); err == nil {
					b.Error("invalid expression accepted")
				}
			}
			b.ReportAllocs()
			if mode == "parallel" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						validate()
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c.Delete("a")
				}
				validate()
			}
		})
	}
}

func TestConcurrentValidationFailuresStayIsolated(t *testing.T) {
	c := NewPreparedReplicationTriggers()
	var calls atomic.Int64
	compiler := triggerValidationFunc(func(expression string) error {
		calls.Add(1)
		return fmt.Errorf("invalid %s", expression)
	})
	var workers sync.WaitGroup
	for index := range 32 {
		workers.Go(func() {
			name := fmt.Sprintf("tenant-%d", index%2)
			spec := validationSpec(1)
			spec.Triggers[0].MatchConditions[0].Expression = name
			for range 20 {
				err := c.Validate(types.UID(name), 1, spec, true, compiler)
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Errorf("validation failure crossed source identities: %v", err)
				}
			}
		})
	}
	workers.Wait()
	if len(c.validations) != 2 || calls.Load() > 32 {
		t.Fatalf("concurrent failure reuse: entries=%d validations=%d", len(c.validations), calls.Load())
	}
}
