// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
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
