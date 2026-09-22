// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestMetadataFailureRetainsEffectivePolicy(t *testing.T) {
	for _, condition := range []string{"", "true"} {
		t.Run(fmt.Sprintf("condition=%q", condition), func(t *testing.T) {
			obj := policyConfigMap("target", "example")
			id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
			opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
			previousApply := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{{
				Manager:   opts.FieldOwnerPrefix + "/" + id.FieldOwner(""),
				Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", Time: &previousApply,
				FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)},
			}})
			failMetadata := false
			patchFailures := 0
			c := fake.NewClientBuilder().WithObjects(obj, &corev1.Namespace{Name: "target"}).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if failMetadata && patch.Type() == types.JSONPatchType {
						patchFailures++
						return errors.New("injected lifecycle metadata failure")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			p, _ := policyProcessor()
			p.GatherClient = c
			var err error
			p.Conditions, err = cache.NewCELCache()
			require.NoError(t, err)
			oldPolicy := &apiruntime.ResourceTemplatePolicy{Condition: "false", Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyOrphan}
			processed := meta.ProcessedItems{{ResourceID: id, LastApply: previousApply, Policy: oldPolicy.DeepCopy()}}
			acc := Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: obj.DeepCopy(), Policy: oldPolicy})
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			actual := obj.DeepCopy()
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.Equal(t, meta.ValueTrue, actual.GetLabels()[meta.ReplicationProtectionLabel])

			failMetadata = true
			desired := policyConfigMap("target", "example")
			desired.Object["data"] = map[string]any{"key": "changed"}
			newPolicy := &apiruntime.ResourceTemplatePolicy{Condition: condition, Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
			acc = Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: desired, Policy: newPolicy})
			require.Error(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			require.Equal(t, 1, patchFailures)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.Equal(t, desired.Object["data"], actual.Object["data"], "SSA completed before the metadata error")
			require.Equal(t, meta.ValueTrue, actual.GetLabels()[meta.ReplicationProtectionLabel], "controller-owned protection remains on target")
			require.False(t, processed[0].LastApply.IsZero())
			require.Equal(t, metav1.ConditionFalse, processed[0].Status)
			for _, global := range []bool{false, true} {
				var parent client.Object
				if global {
					parent = &capsulev1beta2.GlobalTenantResource{Status: capsulev1beta2.GlobalTenantResourceStatus{ProcessedItems: processed}}
				} else {
					parent = &capsulev1beta2.TenantResource{Status: capsulev1beta2.TenantResourceStatus{ProcessedItems: processed}}
				}
				keys := (tenantresource.ProtectedItems{}).Func()(parent)
				if len(keys) != 1 {
					t.Errorf("global=%t: target still protected, but protected-items index returned %v", global, keys)
				}
			}
			require.Equal(t, oldPolicy, processed[0].Policy, "failed metadata reconciliation must retain previous effective policy")
			t.Run("removal follows the previous orphan policy", func(t *testing.T) {
				cleanupClient := fake.NewClientBuilder().WithObjects(actual.DeepCopy(), &corev1.Namespace{Name: "target"}).WithReturnManagedFields().Build()
				cleanupStatus := append(meta.ProcessedItems(nil), processed...)
				require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), cleanupClient, &cleanupStatus, Accumulator{}, opts))
				require.Empty(t, cleanupStatus)
				retained := obj.DeepCopy()
				require.NoError(t, cleanupClient.Get(t.Context(), client.ObjectKeyFromObject(obj), retained))
				require.Equal(t, desired.Object["data"], retained.Object["data"], "failed Remove policy must not prune the orphan's fields")
				require.NotContains(t, retained.GetLabels(), meta.ReplicationProtectionLabel)
			})

			failMetadata = false
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			require.Equal(t, newPolicy, processed[0].Policy, "successful retry commits the new policy")
			require.NotSame(t, newPolicy, processed[0].Policy)
			require.Equal(t, metav1.ConditionTrue, processed[0].Status)
			require.Empty(t, processed[0].Message)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.NotContains(t, actual.GetLabels(), meta.ReplicationProtectionLabel)
			require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
		})
	}
}

func TestMetadataFailureTracksPartiallyCreatedResource(t *testing.T) {
	obj := policyConfigMap("target", "partial")
	p, _ := policyProcessor()
	failMetadata := true
	c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "target"}).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if failMetadata && patch.Type() == types.JSONPatchType {
				return errors.New("injected lifecycle metadata failure")
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	p.GatherClient = c
	id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
	acc := Accumulator{}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: &apiruntime.ResourceTemplatePolicy{Deletion: apiruntime.ResourceDeletionPolicyOrphan}})
	processed := meta.ProcessedItems{}
	opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
	require.Error(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
	require.Len(t, processed, 1)
	require.True(t, processed[0].Created)
	require.False(t, processed[0].LastApply.IsZero(), "partial creation must remain tracked for cleanup")
	require.Nil(t, processed[0].Policy, "the initial policy has not reconciled")
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	failMetadata = false
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
	require.Empty(t, processed)
	require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)), "legacy pruning must clean up the partially created target")
}
