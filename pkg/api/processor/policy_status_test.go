// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func conditionStatusFixture(t testing.TB, count, expressionSize int) (*Processor, *policyRecordingClient, Accumulator, meta.ProcessedItems, *apiruntime.ResourceTemplatePolicy) {
	t.Helper()
	policy := &apiruntime.ResourceTemplatePolicy{Condition: "false //" + strings.Repeat("x", expressionSize-8), Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan}
	objects := make([]client.Object, 0, count)
	acc := Accumulator{}
	items := make(meta.ProcessedItems, 0, count)
	for i := range count {
		obj := policyConfigMap("target", fmt.Sprintf("item-%d", i))
		id := gvk.NewResourceID(obj, "tenant-a", "0/raw")
		timestamp := metav1.Now()
		obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "/" + id.FieldOwner(""), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", Time: &timestamp, FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}})
		objects = append(objects, obj)
		items = append(items, meta.ObjectReferenceStatus{ResourceID: id, LastApply: timestamp, Policy: policy.DeepCopy()})
		AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: policy})
	}
	p, c := policyProcessor(objects...)
	var err error
	p.Conditions, err = cache.NewCELCache()
	require.NoError(t, err)
	return p, c, acc, items, policy
}

func TestConditionIsNotRepeatedInProcessedStatus(t *testing.T) {
	p, c, acc, processed, policy := conditionStatusFixture(t, 300, 4096)
	original := policy.DeepCopy()
	before, err := json.Marshal(processed)
	require.NoError(t, err)
	require.Greater(t, len(before), 1024*1024, "fixture must reproduce the large repeated status")
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}))
	after, err := json.Marshal(processed)
	require.NoError(t, err)
	require.Less(t, len(after), 200*1024)
	require.NotContains(t, string(after), `"condition"`)
	require.Equal(t, original, policy, "the source expression must remain unchanged")
	require.Empty(t, c.applies, "the source condition must still gate content")
	for _, item := range processed {
		require.NotNil(t, item.Policy)
		require.Empty(t, item.Policy.Condition)
		require.True(t, item.Policy.ShouldOrphan())
		require.False(t, item.Policy.IsProtected())
		require.NotSame(t, policy.Protect, item.Policy.Protect)
	}
	t.Logf("300 targets with a 4096-byte condition: status shrank from %d to %d bytes", len(before), len(after))
}

// Includes processor reconciliation and serialization, with the compiled
// expression cached as it is during steady-state reconciliation.
func BenchmarkProcessorConditionStatus(b *testing.B) {
	for _, count := range []int{1, 300} {
		for _, size := range []int{16, 4096} {
			b.Run(fmt.Sprintf("items=%d/expression=%d", count, size), func(b *testing.B) {
				p, c, acc, processed, _ := conditionStatusFixture(b, count, size)
				require.NoError(b, p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}))
				bytes := 0
				b.ReportAllocs()
				for b.Loop() {
					if err := p.Reconcile(b.Context(), logr.Discard(), c, &processed, acc, ProcessorOptions{}); err != nil {
						b.Fatal(err)
					}
					data, err := json.Marshal(processed)
					if err != nil {
						b.Fatal(err)
					}
					bytes = len(data)
				}
				b.ReportMetric(float64(bytes), "status-B")
			})
		}
	}
}
