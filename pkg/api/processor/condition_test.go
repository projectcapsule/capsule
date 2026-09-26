// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

// Measures the processor and fake-client plumbing, not API-server latency.
func BenchmarkProcessorCondition(b *testing.B) {
	for _, items := range []int{1, 100} {
		for _, condition := range []string{"false", "true"} {
			b.Run(fmt.Sprintf("items=%d/condition=%s", items, condition), func(b *testing.B) {
				objects := make([]client.Object, 0, items)
				acc := Accumulator{}
				for i := range items {
					obj := policyConfigMap("target", fmt.Sprintf("conditional-%d", i))
					obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications})
					objects = append(objects, obj)
					AccumulatorAdd(acc, gvk.NewResourceID(obj, "tenant-a", "0/raw"), AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Condition: condition}})
				}
				p, c := policyProcessor(objects...)
				conditions, err := cache.NewCELCache()
				if err != nil {
					b.Fatal(err)
				}
				p.Conditions = conditions
				processed := meta.ProcessedItems{}
				if err := p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					c.applies = c.applies[:0]
					c.force = c.force[:0]
					if err := p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err != nil {
						b.Fatal(err)
					}
					if len(processed) != items || condition == "false" && len(c.applies) != 0 {
						b.Fatal("incorrect conditional processing")
					}
				}
			})
		}
	}
}

func TestSkippedConditionUpdatesPolicyUntilRemoval(t *testing.T) {
	obj := policyConfigMap("target", "example")
	obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications, meta.ProtectedByCapsuleLabel: meta.ValueControllerReplications})
	conditions, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
	oldPolicy := &apiruntime.ResourceReplicationPolicy{Protect: new(true)}
	old := meta.ObjectReferenceStatus{ResourceID: id, LastApply: metav1.NewTime(time.Now().UTC().Truncate(time.Second)), Created: true, Policy: processedPolicy(oldPolicy), Type: meta.ReadyCondition, Status: metav1.ConditionTrue}
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "/" + id.FieldOwner(""), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", Time: &old.LastApply, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}})
	p, c := policyProcessor(obj)
	p.Conditions = conditions
	processed := meta.ProcessedItems{old}
	acc := Accumulator{}
	policy := &apiruntime.ResourceReplicationPolicy{Condition: "false", Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: policy})
	c.metadataError = errors.New("policy write denied")
	if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{Prune: true}); err == nil {
		t.Fatal("policy patch failure was ignored")
	}
	if !reflect.DeepEqual(processed[0].Policy, processedPolicy(oldPolicy)) || !processed[0].LastApply.Equal(&old.LastApply) {
		t.Fatal("failed metadata write changed the effective policy or content timestamp")
	}
	c.metadataError = nil
	c.metadataPatches = 0
	expectedPolicy := processedPolicy(policy)
	for range 2 {
		if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{Prune: true}); err != nil {
			t.Fatal(err)
		}
		if len(c.applies) != 0 || len(processed) != 1 || processed[0].Message != ssa.ConditionNotMet || !processed[0].Created || !processed[0].LastApply.Equal(&old.LastApply) || !reflect.DeepEqual(processed[0].Policy, expectedPolicy) {
			t.Fatalf("skip did not update policy while retaining content apply state: applies=%d processed=%+v policy=%+v old=%+v", len(c.applies), processed, processed[0].Policy, old)
		}
		current := policyConfigMap("target", "example")
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(current), current); err != nil {
			t.Fatal(err)
		}
		if current.GetLabels()[meta.ProtectedByCapsuleLabel] != "" {
			t.Fatal("skip did not update protection")
		}
	}
	if c.metadataPatches != 1 {
		t.Fatalf("policy change should patch once, then skip without writing: %d", c.metadataPatches)
	}
	if err := p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, ProcessorOptions{Prune: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj); err != nil {
		t.Fatalf("removal did not follow updated Orphan policy: %v", err)
	}
	if obj.GetLabels()[meta.NewManagedByCapsuleLabel] != "" || obj.GetLabels()[meta.CreatedByCapsuleLabel] != "" {
		t.Fatal("orphan retained lifecycle metadata")
	}
}
