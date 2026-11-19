// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

type healthCountingClient struct {
	client.Client
	reads int
}

func (c *healthCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.reads++
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestProcessorCapturesHealthWithoutAdditionalReads(t *testing.T) {
	counts := []int{}
	for _, enabled := range []bool{false, true} {
		obj := policyConfigMap("target", "example")
		obj.SetUID("target-uid")
		obj.SetGeneration(3)
		p, c := policyProcessor(obj.DeepCopy())
		counter := &healthCountingClient{Client: c}
		id := gvk.NewResourceID(obj, "tenant-a", "raw")
		acc := Accumulator{}
		AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge}})
		items := meta.ProcessedItems{}
		require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), counter, &items, acc, ProcessorOptions{TrackHealth: enabled, FieldOwnerPrefix: "test"}))
		require.Len(t, items, 1)
		counts = append(counts, counter.reads)
		if enabled {
			require.NotNil(t, items[0].Health)
			require.Equal(t, obj.GetUID(), items[0].Health.UID)
			require.Equal(t, int64(3), items[0].Health.Generation)
			require.Len(t, items[0].Health.Digest, 64)
		} else {
			require.Nil(t, items[0].Health)
		}
	}
	require.Equal(t, counts[0], counts[1])
}

func TestProcessorSkippedHealthAndOpaqueContentRevision(t *testing.T) {
	obj := policyConfigMap("target", "example")
	obj.SetUID("uid")
	p, c := policyProcessor(obj.DeepCopy())
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	p.Conditions = conditions
	acc := Accumulator{}
	AccumulatorAdd(acc, gvk.NewResourceID(obj, "tenant-a", "raw"), AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Condition: "false"}})
	items := meta.ProcessedItems{}
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &items, acc, ProcessorOptions{TrackHealth: true, FieldOwnerPrefix: "test"}))
	require.Nil(t, items[0].Health)

	first := appliedHealthReference(ssa.ApplyResult{UID: "uid", ContentVersion: "opaque-one", ContentChanged: true}, nil)
	unchanged := appliedHealthReference(ssa.ApplyResult{UID: "uid", ContentVersion: "metadata-only-change"}, first)
	require.Same(t, first, unchanged)
	changed := appliedHealthReference(ssa.ApplyResult{UID: "uid", ContentVersion: "opaque-two", ContentChanged: true}, first)
	require.NotEqual(t, first.Digest, changed.Digest)
	composed := appliedHealthReference(ssa.ApplyResult{UID: "uid", ContentVersion: "opaque-two"}, changed)
	require.Equal(t, changed.Digest, composed.Digest)
}

func TestApplyRetryRetainsChangedContentAcrossNoopRetry(t *testing.T) {
	initial := policyConfigMap("target", "example")
	initial.SetUID("target-uid")
	applies, conflicts := 0, 0
	c := fake.NewClientBuilder().WithObjects(initial).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if patch.Type() == types.ApplyPatchType {
			applies++
			if applies > 1 {
				// Kubernetes returns the unchanged live object for a no-op SSA retry.
				fresh := initial.DeepCopy()
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
					return err
				}
				*obj.(*unstructured.Unstructured) = *fresh
				return nil
			}
		}
		if patch.Type() == types.JSONPatchType && (&client.PatchOptions{}).ApplyOptions(opts).FieldManager == meta.ResourceControllerFieldOwnerPrefix() && conflicts == 0 {
			conflicts++
			return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("metadata raced"))
		}
		return c.Patch(ctx, obj, patch, opts...)
	}}).Build()
	desired := initial.DeepCopy()
	desired.Object["data"] = map[string]any{"key": "changed"}
	p := &Processor{}
	result, err := p.Apply(t.Context(), c, desired, "health-test", true, true, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, conflicts)
	require.Equal(t, 2, applies)
	require.True(t, result.ContentChanged)
	require.NotEmpty(t, result.ContentVersion)
	require.Equal(t, initial.GetUID(), result.UID)
}

func TestFailedApplyRetainsExistingHealthSnapshot(t *testing.T) {
	obj := policyConfigMap("target", "example")
	obj.SetUID("target-uid")
	p, c := policyProcessor(obj)
	c.applyError = errors.New("temporary write failure")
	id := gvk.NewResourceID(obj, "tenant-a", "raw")
	previous := &meta.ResourceHealthReference{UID: obj.GetUID(), Digest: "previous"}
	items := meta.ProcessedItems{{ResourceID: id, Health: previous}}
	acc := Accumulator{}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge}})
	require.Error(t, p.Reconcile(t.Context(), logr.Discard(), c, &items, acc, ProcessorOptions{TrackHealth: true, FieldOwnerPrefix: "test"}))
	require.Equal(t, previous, items[0].Health)
}

func TestSharedTargetHealthUsesFinalAppliedGeneration(t *testing.T) {
	counts := []int{}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			obj := policyConfigMap("target", "example")
			obj.SetUID("target-uid")
			obj.SetGeneration(1)
			p, base := policyProcessor(obj.DeepCopy())
			generation := int64(1)
			c := &healthCountingClient{Client: interceptor.NewClient(base.Client.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if err := c.Patch(ctx, obj, patch, opts...); err != nil {
					return err
				}
				if patch.Type() == types.ApplyPatchType {
					generation++
					obj.SetGeneration(generation)
					return c.Update(ctx, obj)
				}
				return nil
			}})}
			acc := Accumulator{}
			for i := range 2 {
				contribution := obj.DeepCopy()
				contribution.Object["data"] = map[string]any{fmt.Sprintf("field-%d", i): "value"}
				id := gvk.NewResourceID(contribution, "tenant-a", fmt.Sprintf("origin-%d", i))
				AccumulatorAdd(acc, id, AccumulatorObject{Object: contribution, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false)}})
			}
			items := meta.ProcessedItems{}
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &items, acc, ProcessorOptions{TrackHealth: enabled, FieldOwnerPrefix: "shared-health"}))
			require.Len(t, items, 2)
			require.NotEqual(t, items[0].Origin, items[1].Origin, "ownership records must remain separate")
			counts = append(counts, c.reads)
			for _, item := range items {
				if enabled {
					require.Equal(t, generation, item.Health.Generation)
					require.Equal(t, items[0].Health.Digest, item.Health.Digest)
				} else {
					require.Nil(t, item.Health)
				}
			}
			if enabled {
				require.NotSame(t, items[0].Health, items[1].Health)
			}
		})
	}
	require.Equal(t, counts[0], counts[1], "finalizing shared health must not read targets again")
}

func TestSharedHealthRetainsContentWriteAcrossNoopContributions(t *testing.T) {
	id := gvk.ResourceID{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "a", Name: "shared", Tenant: "a", Origin: "first"}
	other := id
	other.Origin = "second"
	targets := appliedHealthTargets{}
	old := &meta.ResourceHealthReference{UID: "uid", Digest: "old"}
	targets.record(id, ssa.ApplyResult{UID: "uid", ContentChanged: true, ContentVersion: "content-write"}, old)
	targets.record(other, ssa.ApplyResult{UID: "uid", ContentVersion: "metadata-only"}, old)
	items := meta.ProcessedItems{{ResourceID: id, Health: old}, {ResourceID: other, Health: old}, {ResourceID: other}, {ResourceID: id, Health: &meta.ResourceHealthReference{UID: "replacement", Digest: "different"}}}
	targets.finalize(items)
	require.Equal(t, appliedHealthReference(ssa.ApplyResult{UID: "uid", ContentChanged: true, ContentVersion: "content-write"}, nil), items[0].Health)
	require.Equal(t, items[0].Health, items[1].Health)
	require.Nil(t, items[2].Health, "skipped contributions must remain ineligible")
	require.Equal(t, "different", items[3].Health.Digest, "a replacement UID must not inherit another target's snapshot")
	require.Equal(t, "old", old.Digest, "previous snapshots must remain immutable")
}

func TestSharedHealthRetainsSuccessfulWritesAcrossPartialFailure(t *testing.T) {
	obj := policyConfigMap("target", "example")
	obj.SetUID("target-uid")
	p, base := policyProcessor(obj.DeepCopy())
	applies := 0
	c := interceptor.NewClient(base.Client.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if patch.Type() == types.ApplyPatchType {
			applies++
			if applies == 2 {
				return errors.New("temporary apply failure")
			}
		}
		return c.Patch(ctx, obj, patch, opts...)
	}})
	items := meta.ProcessedItems{}
	acc := Accumulator{}
	for i := range 2 {
		id := gvk.NewResourceID(obj, "tenant-a", fmt.Sprint(i))
		items = append(items, meta.ObjectReferenceStatus{ResourceID: id, Health: &meta.ResourceHealthReference{UID: obj.GetUID(), Digest: "old"}})
		AccumulatorAdd(acc, id, AccumulatorObject{Object: obj.DeepCopy(), Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false)}})
	}
	require.Error(t, p.Reconcile(t.Context(), logr.Discard(), c, &items, acc, ProcessorOptions{TrackHealth: true, FieldOwnerPrefix: "partial-shared"}))
	require.Equal(t, items[0].Health, items[1].Health)
	require.NotEqual(t, "old", items[0].Health.Digest)
	require.NotEqual(t, items[0].Status, items[1].Status, "application outcomes remain per origin")
}

func BenchmarkSharedHealthFinalization(b *testing.B) {
	for _, count := range []int{1, 128, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			items := make(meta.ProcessedItems, count)
			id := gvk.ResourceID{Kind: "ConfigMap", Namespace: "a", Name: "shared"}
			for i := range items {
				items[i] = meta.ObjectReferenceStatus{ResourceID: id, Health: &meta.ResourceHealthReference{UID: "uid"}}
				items[i].Origin = fmt.Sprint(i)
			}
			targets := appliedHealthTargets{}
			generation := int64(0)
			b.ReportAllocs()
			for b.Loop() {
				generation++
				targets.record(id, ssa.ApplyResult{UID: "uid", Generation: generation, ContentChanged: true, ContentVersion: "write"}, nil)
				targets.finalize(items)
			}
		})
	}
}

func BenchmarkProcessorSharedHealth(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		for _, origins := range []int{1, 16} {
			b.Run(fmt.Sprintf("health=%t/origins=%d", enabled, origins), func(b *testing.B) {
				obj := policyConfigMap("target", "example")
				obj.SetUID("target-uid")
				p, base := policyProcessor(obj.DeepCopy())
				c := &healthCountingClient{Client: base}
				acc := Accumulator{}
				for i := range origins {
					contribution := obj.DeepCopy()
					contribution.Object["data"] = map[string]any{fmt.Sprint(i): "value"}
					AccumulatorAdd(acc, gvk.NewResourceID(contribution, "tenant-a", fmt.Sprint(i)), AccumulatorObject{Object: contribution, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false)}})
				}
				items := meta.ProcessedItems{}
				opts := ProcessorOptions{TrackHealth: enabled, FieldOwnerPrefix: "benchmark-health"}
				if err := p.Reconcile(b.Context(), logr.Discard(), c, &items, acc, opts); err != nil {
					b.Fatal(err)
				}
				c.reads = 0
				base.applies, base.force = nil, nil
				b.ReportAllocs()
				for b.Loop() {
					if err := p.Reconcile(b.Context(), logr.Discard(), c, &items, acc, opts); err != nil {
						b.Fatal(err)
					}
					base.applies, base.force = base.applies[:0], base.force[:0]
				}
				b.ReportMetric(float64(c.reads)/float64(b.N), "GET/op")
			})
		}
	}
}
