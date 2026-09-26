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
			oldPolicy := &apiruntime.ResourceReplicationPolicy{Condition: "false", Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyOrphan}
			processed := meta.ProcessedItems{{ResourceID: id, LastApply: previousApply, Policy: processedPolicy(oldPolicy)}}
			acc := Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: obj.DeepCopy(), Policy: oldPolicy})
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			actual := obj.DeepCopy()
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.Equal(t, meta.ValueTrue, actual.GetLabels()[meta.ReplicationProtectionLabel])

			failMetadata = true
			desired := policyConfigMap("target", "example")
			desired.Object["data"] = map[string]any{"key": "changed"}
			newPolicy := &apiruntime.ResourceReplicationPolicy{Condition: condition, Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
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
			expectedOld := processedPolicy(oldPolicy)
			require.Equal(t, expectedOld, processed[0].Policy, "failed metadata reconciliation must retain previous effective policy")
			require.Equal(t, "false", oldPolicy.Condition, "status projection must not mutate the input policy")
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
			expectedNew := processedPolicy(newPolicy)
			require.Equal(t, expectedNew, processed[0].Policy, "successful retry commits the new policy")
			require.NotSame(t, newPolicy, processed[0].Policy)
			require.Equal(t, metav1.ConditionTrue, processed[0].Status)
			require.Empty(t, processed[0].Message)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.NotContains(t, actual.GetLabels(), meta.ReplicationProtectionLabel)
			require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
		})
	}
}

func TestMetadataFailureTracksFirstApplyPolicy(t *testing.T) {
	for _, condition := range []string{"", "true"} {
		for _, tc := range []struct {
			name                       string
			adopted, previouslySkipped bool
			policy                     *apiruntime.ResourceReplicationPolicy
		}{
			{name: "created orphan", policy: &apiruntime.ResourceReplicationPolicy{Deletion: apiruntime.ResourceDeletionPolicyOrphan}},
			{name: "created remove", policy: &apiruntime.ResourceReplicationPolicy{Deletion: apiruntime.ResourceDeletionPolicyRemove}},
			{name: "adopted orphan", adopted: true, policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Deletion: apiruntime.ResourceDeletionPolicyOrphan}},
			{name: "previous skip then orphan", previouslySkipped: true, policy: &apiruntime.ResourceReplicationPolicy{Deletion: apiruntime.ResourceDeletionPolicyOrphan}},
			{name: "legacy creation"},
		} {
			t.Run(fmt.Sprintf("%s/condition=%q", tc.name, condition), func(t *testing.T) {
				obj := policyConfigMap("target", "partial")
				objects := []client.Object{&corev1.Namespace{Name: "target"}}
				if tc.adopted {
					objects = append(objects, obj.DeepCopy())
				}
				failMetadata := true
				c := fake.NewClientBuilder().WithObjects(objects...).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						options := (&client.PatchOptions{}).ApplyOptions(opts)
						if failMetadata && patch.Type() == types.JSONPatchType && options.FieldManager == meta.ResourceControllerFieldOwnerPrefix() {
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
				policy := tc.policy.DeepCopy()
				if policy != nil {
					policy.Condition = condition
				}
				id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
				acc := Accumulator{}
				AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: policy})
				processed := meta.ProcessedItems{}
				if tc.previouslySkipped {
					processed = append(processed, meta.ObjectReferenceStatus{ResourceID: id})
				}
				opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
				require.Error(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
				require.Len(t, processed, 1)
				require.Equal(t, !tc.adopted, processed[0].Created)
				require.False(t, processed[0].LastApply.IsZero(), "partial apply must remain tracked for cleanup")
				require.Equal(t, processedPolicy(tc.policy), processed[0].Policy, "the first content write must preserve its lifecycle policy")
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
				failMetadata = false
				require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
				require.Empty(t, processed)
				err = c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)
				if tc.policy != nil && tc.policy.ShouldOrphan() {
					require.NoError(t, err, "explicit Orphan must survive cleanup before a retry")
					require.Equal(t, map[string]any{"key": "value"}, obj.Object["data"])
					require.NotContains(t, obj.GetLabels(), meta.ReplicationProtectionLabel)
				} else {
					require.True(t, apierrors.IsNotFound(err), "Remove and legacy pruning must clean up the created target")
				}
			})
		}
	}
}

func TestMetadataFailureRetainsLegacyProtection(t *testing.T) {
	obj := policyConfigMap("target", "legacy")
	id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
	opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg"}
	obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications, meta.ReplicationProtectionLabel: meta.ValueTrue})
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: opts.FieldOwnerPrefix + "/" + id.FieldOwner(""), Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}},
		{Manager: meta.ResourceControllerFieldOwnerPrefix(), Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:protection.projectcapsule.dev/replications":{},"f:projectcapsule.dev/created-by":{}}}}`)}},
	})
	c := fake.NewClientBuilder().WithObjects(obj, &corev1.Namespace{Name: "target"}).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if patch.Type() == types.JSONPatchType {
				return errors.New("injected lifecycle metadata failure")
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	p, _ := policyProcessor()
	p.GatherClient = c
	old := meta.ObjectReferenceStatus{ResourceID: id, Created: true, LastApply: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	processed := meta.ProcessedItems{old}
	acc := Accumulator{}
	desired := policyConfigMap("target", "legacy")
	desired.Object["data"] = map[string]any{"key": "updated"}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: desired, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan}})
	require.ErrorContains(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts), "applying of 1 resources failed")
	require.Contains(t, processed[0].Message, "injected lifecycle metadata failure")
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.Equal(t, desired.Object["data"], obj.Object["data"])
	require.True(t, processed[0].LastApply.After(old.LastApply.Time), "exercise the error path after a successful content write")
	require.Nil(t, processed[0].Policy, "nil is a valid effective policy for an already applied legacy item")
	parent := &capsulev1beta2.TenantResource{Status: capsulev1beta2.TenantResourceStatus{ProcessedItems: processed}}
	require.Len(t, (tenantresource.ProtectedItems{}).Func()(parent), 1, "legacy protection must remain indexed")
}

func TestAdoptionSurvivesCreationPolicyTransition(t *testing.T) {
	for _, skipped := range []bool{false, true} {
		t.Run(fmt.Sprintf("skipped=%t", skipped), func(t *testing.T) {
			p, _ := policyProcessor()
			c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "target"}).WithReturnManagedFields().Build()
			p.GatherClient = c
			existing := policyConfigMap("target", "adopted")
			require.NoError(t, c.Patch(t.Context(), existing, client.Apply, client.FieldOwner("external")))
			desired := policyConfigMap("target", "adopted")
			desired.Object["data"] = map[string]any{"managed": "capsule"}
			id := gvk.NewResourceID(desired, "tenant-a", "0/raw-0")
			opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
			policy := &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
			acc := Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: desired, Policy: policy})
			processed := meta.ProcessedItems{}
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			require.False(t, processed[0].Created)
			if skipped {
				p.Conditions, _ = cache.NewCELCache()
				policy.Condition = "false"
				require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			}
			policy.Creation = apiruntime.ResourceCreationPolicyOwner
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			policy.Condition = "true"
			p.Conditions, _ = cache.NewCELCache()
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			// Restart without processed status: live adoption must still remain adoption.
			processed = nil
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
			incorrectlyCreated := processed[0].Created
			actual := desired.DeepCopy()
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(actual), actual))
			t.Logf("after Merge->Owner: Created=%t, labels=%v", incorrectlyCreated, actual.GetLabels())
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
			deleted := apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(actual), actual))
			t.Logf("Remove cleanup deleted adopted target=%t", deleted)
			require.False(t, incorrectlyCreated, "changing creation policy must not reclassify an adopted object as created")
			require.False(t, deleted, "cleanup must not delete the external object")
		})
	}
}

func TestSkippedReplacementDoesNotInheritCreation(t *testing.T) {
	p, _ := policyProcessor()
	c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "target"}).WithReturnManagedFields().Build()
	p.GatherClient = c
	var err error
	p.Conditions, err = cache.NewCELCache()
	require.NoError(t, err)
	desired := policyConfigMap("target", "replaced")
	id := gvk.NewResourceID(desired, "tenant-a", "0/raw-0")
	opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
	policy := &apiruntime.ResourceReplicationPolicy{Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
	acc := Accumulator{}
	AccumulatorAdd(acc, id, AccumulatorObject{Object: desired, Policy: policy})
	processed := meta.ProcessedItems{}
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
	require.True(t, processed[0].Created)
	require.NoError(t, c.Delete(t.Context(), desired))
	foreign := policyConfigMap("target", "replaced")
	foreign.SetUID("replacement-uid")
	foreign.Object["data"] = map[string]any{"foreign": "retained"}
	require.NoError(t, c.Create(t.Context(), foreign))
	require.NoError(t, c.Patch(t.Context(), foreign, client.Apply, client.FieldOwner("external")))
	for _, field := range foreign.GetManagedFields() {
		require.NotEqual(t, opts.FieldOwnerPrefix+"/"+id.FieldOwner(""), field.Manager)
	}
	policy.Condition = "false"
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts))
	staleCreated := processed[0].Created
	t.Logf("condition skipped unowned replacement: Created=%t, LastApply=%v", staleCreated, processed[0].LastApply)
	require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
	deleted := apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign))
	t.Logf("Remove cleanup deleted replacement=%t", deleted)
	require.False(t, staleCreated, "an existing unowned replacement must not inherit the old Created state")
	require.False(t, deleted, "must retain the replacement object")
}
