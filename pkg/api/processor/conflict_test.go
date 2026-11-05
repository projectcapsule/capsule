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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestApplyRetriesFreshTargetAfterConflict(t *testing.T) {
	for _, stage := range []string{"ownership", "apply", "metadata"} {
		t.Run(stage, func(t *testing.T) {
			const owner = "replication/test"
			conflicts, applies := 0, 0
			var staleVersion string
			builder := fake.NewClientBuilder().WithReturnManagedFields()
			if stage == "ownership" {
				initial := policyConfigMap("tenant-a", "target")
				initial.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications})
				initial.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: owner, Operation: metav1.ManagedFieldsOperationUpdate, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}})
				builder.WithObjects(initial)
			}
			c := builder.WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					manager := (&client.PatchOptions{}).ApplyOptions(opts).FieldManager
					matches := stage == "apply" && patch.Type() == types.ApplyPatchType || stage == "ownership" && patch.Type() == types.JSONPatchType && manager == owner || stage == "metadata" && patch.Type() == types.JSONPatchType && manager == meta.ResourceControllerFieldOwnerPrefix()
					if patch.Type() == types.ApplyPatchType {
						applies++
						if conflicts > 0 {
							require.NotEqual(t, staleVersion, obj.GetResourceVersion(), "retried a stale SSA payload")
						}
					}
					if matches && conflicts == 0 {
						conflicts++
						staleVersion = obj.GetResourceVersion()
						actual := policyConfigMap(obj.GetNamespace(), obj.GetName())
						require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), actual))
						before := actual.DeepCopy()
						actual.SetAnnotations(map[string]string{"external.example/change": "retained"})
						require.NoError(t, c.Patch(ctx, actual, client.MergeFrom(before), client.FieldOwner("external")))
						if patch.Type() == types.JSONPatchType {
							err := c.Patch(ctx, obj, patch, opts...)
							require.True(t, apierrors.IsConflict(err), "stale metadata must produce a retryable conflict: %v", err)
							return err
						}
						return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent write"))
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			p := &Processor{}
			obj := policyConfigMap("tenant-a", "target")
			before := obj.DeepCopy()
			result, err := p.Apply(t.Context(), c, obj, owner, false, false, nil, nil, nil)
			require.NoError(t, err)
			require.Equal(t, 1, conflicts)
			require.True(t, result.Created, "lost first-creation provenance")
			require.Equal(t, before, obj, "mutated caller-owned input")
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
			require.Equal(t, "retained", obj.GetAnnotations()["external.example/change"])
			require.Equal(t, meta.ValueControllerReplications, obj.GetLabels()[meta.CreatedByCapsuleLabel])
			require.GreaterOrEqual(t, applies, 1)
		})
	}
}

func TestApplyConflictReevaluatesCondition(t *testing.T) {
	obj := policyConfigMap("tenant-a", "target")
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "replication/test", Operation: metav1.ManagedFieldsOperationApply, APIVersion: "v1", FieldsType: "FieldsV1", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:key":{}}}`)}}})
	patches := 0
	c := fake.NewClientBuilder().WithObjects(obj).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, target client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if patch.Type() == types.ApplyPatchType {
				patches++
				actual := obj.DeepCopy()
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), actual))
				before := actual.DeepCopy()
				require.NoError(t, unstructured.SetNestedField(actual.Object, "blocked", "data", "key"))
				require.NoError(t, c.Patch(ctx, actual, client.MergeFrom(before)))
				return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("condition changed"))
			}
			return c.Patch(ctx, target, patch, opts...)
		},
	}).Build()
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	p := &Processor{Conditions: compiler}
	result, err := p.Apply(t.Context(), c, obj, "replication/test", false, false, nil, nil, &apiruntime.ResourceReplicationPolicy{Condition: "object.data.key == 'value'"})
	require.NoError(t, err)
	require.True(t, result.Skipped)
	require.Equal(t, 1, patches, "condition no longer permits applying content")
}

func TestApplyConflictRechecksReplacementOwnership(t *testing.T) {
	for _, adopt := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject replacement", true: "adopt without creation provenance"}[adopt], func(t *testing.T) {
			obj := policyConfigMap("tenant-a", "target")
			patches := 0
			c := fake.NewClientBuilder().WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, target client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if patch.Type() == types.ApplyPatchType {
						patches++
						if patches == 1 {
							require.NoError(t, c.Delete(ctx, obj))
							replacement := policyConfigMap(obj.GetNamespace(), obj.GetName())
							replacement.SetUID("replacement-uid")
							replacement.SetAnnotations(map[string]string{"external.example/replacement": "retained"})
							require.NoError(t, c.Create(ctx, replacement))
							return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("target replaced"))
						}
					}
					return c.Patch(ctx, target, patch, opts...)
				},
			}).Build()
			result, err := (&Processor{}).Apply(t.Context(), c, obj, "replication/test", false, adopt, nil, nil, nil)
			if adopt {
				require.NoError(t, err)
				require.Equal(t, 2, patches)
			} else {
				require.ErrorContains(t, err, "cannot be adopted")
				require.Equal(t, 1, patches, "must reject replacement before another content write")
			}
			require.False(t, result.Created, "creation provenance belongs to the original UID")
			actual := obj.DeepCopy()
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), actual))
			require.Equal(t, types.UID("replacement-uid"), actual.GetUID())
			require.Equal(t, "retained", actual.GetAnnotations()["external.example/replacement"])
			require.NotContains(t, actual.GetLabels(), meta.CreatedByCapsuleLabel)
		})
	}
}

func TestApplyDoesNotRetryPermanentErrors(t *testing.T) {
	for name, failure := range map[string]error{
		"field ownership": &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonConflict, Code: 409, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict}}}}},
		"forbidden":       apierrors.NewForbidden(corev1.Resource("configmaps"), "target", errors.New("denied")),
		"invalid":         apierrors.NewBadRequest("invalid content"),
		"invalid patch":   &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonInvalid, Code: 422}},
	} {
		t.Run(name, func(t *testing.T) {
			patches := 0
			c := fake.NewClientBuilder().WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
				patches++
				return failure
			}}).Build()
			_, err := (&Processor{}).Apply(t.Context(), c, policyConfigMap("tenant-a", "target"), "test", false, false, nil, nil, nil)
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, patches)
		})
	}
}

func TestExhaustedApplyConflictsKeepInitializationForCleanup(t *testing.T) {
	for _, deletion := range []apiruntime.ResourceDeletionPolicy{apiruntime.ResourceDeletionPolicyRemove, apiruntime.ResourceDeletionPolicyOrphan} {
		t.Run(string(deletion), func(t *testing.T) {
			attempts := 0
			created := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "tenant-a"}).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					obj.SetUID("target-uid")
					obj.SetCreationTimestamp(created)
					return c.Create(ctx, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if patch.Type() == types.ApplyPatchType {
						attempts++
						return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent status writer"))
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			p, _ := policyProcessor()
			p.GatherClient = c
			obj := policyConfigMap("tenant-a", "target")
			id := gvk.NewResourceID(obj, "tenant-a", "0/raw-0")
			policy := &apiruntime.ResourceReplicationPolicy{Deletion: deletion}
			acc := Accumulator{}
			AccumulatorAdd(acc, id, AccumulatorObject{Object: obj, Policy: policy})
			processed := meta.ProcessedItems{}
			opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
			require.ErrorContains(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts), "applying of 1 resources failed")
			require.Equal(t, retry.DefaultBackoff.Steps, attempts)
			require.Len(t, processed, 1)
			require.True(t, processed[0].LastApply.Equal(&created), "initialization must stay tracked when every SSA attempt conflicts")
			require.True(t, processed[0].Created)
			require.Equal(t, processedPolicy(policy), processed[0].Policy)
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
			require.Empty(t, processed)
			err := c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)
			if deletion == apiruntime.ResourceDeletionPolicyOrphan {
				require.NoError(t, err)
				require.NotContains(t, obj.GetLabels(), meta.ReplicationProtectionLabel)
			} else {
				require.True(t, apierrors.IsNotFound(err), "cleanup must remove the initialized resource: %v", err)
			}
		})
	}
}

func TestApplyConflictRetryIsBoundedAndCancelable(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "cancelled"}[cancel], func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			patches := 0
			conflict := apierrors.NewConflict(corev1.Resource("configmaps"), "target", errors.New("busy"))
			c := fake.NewClientBuilder().WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
				patches++
				if cancel {
					stop()
				}
				return conflict
			}}).Build()
			_, err := (&Processor{}).Apply(ctx, c, policyConfigMap("tenant-a", "target"), "test", false, false, nil, nil, nil)
			if cancel {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, patches)
			} else {
				require.ErrorIs(t, err, conflict)
				require.Equal(t, retry.DefaultBackoff.Steps, patches)
			}
		})
	}
}

func TestFailedObservationKeepsCreationForCleanup(t *testing.T) {
	for _, condition := range []string{"", "true"} {
		for _, deletion := range []apiruntime.ResourceDeletionPolicy{apiruntime.ResourceDeletionPolicyRemove, apiruntime.ResourceDeletionPolicyOrphan} {
			t.Run(condition+"/"+string(deletion), func(t *testing.T) {
				created := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
				attempts, failedReads := 0, 0
				cleanup := false
				c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "tenant-a"}).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if !cleanup && key.Name == "target" && attempts > 0 {
							failedReads++
							return apierrors.NewServiceUnavailable("target read unavailable")
						}
						return c.Get(ctx, key, obj, opts...)
					},
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						obj.SetUID("created-uid")
						obj.SetCreationTimestamp(created)
						return c.Create(ctx, obj, opts...)
					},
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if patch.Type() == types.ApplyPatchType && !cleanup {
							attempts++
							return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent writer"))
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).Build()
				conditions, err := cache.NewCELCache()
				require.NoError(t, err)
				p, _ := policyProcessor()
				p.GatherClient, p.Conditions = c, conditions
				obj := policyConfigMap("tenant-a", "target")
				policy := &apiruntime.ResourceReplicationPolicy{Condition: condition, Deletion: deletion}
				acc := Accumulator{}
				AccumulatorAdd(acc, gvk.NewResourceID(obj, "tenant-a", "0/raw-0"), AccumulatorObject{Object: obj, Policy: policy})
				processed := meta.ProcessedItems{}
				opts := ProcessorOptions{FieldOwnerPrefix: "2lclct9cwq6mg", Prune: true}
				require.ErrorContains(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, acc, opts), "applying of 1 resources failed")
				require.Equal(t, 1, attempts)
				require.Equal(t, 1, failedReads, "observation failures must not be retried as conflicts")
				require.Len(t, processed, 1)
				require.Contains(t, processed[0].Message, "target read unavailable")
				require.True(t, processed[0].Created)
				require.True(t, processed[0].LastApply.Equal(&created))
				require.Equal(t, processedPolicy(policy), processed[0].Policy)
				cleanup = true
				require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), c, &processed, Accumulator{}, opts))
				require.Empty(t, processed)
				err = c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)
				if deletion == apiruntime.ResourceDeletionPolicyRemove {
					require.True(t, apierrors.IsNotFound(err))
				} else {
					require.NoError(t, err)
					require.Equal(t, map[string]any{"key": "value"}, obj.Object["data"])
					require.NotContains(t, obj.GetLabels(), meta.ReplicationProtectionLabel)
				}
			})
		}
	}
}

func TestApplyConflictTimestampIsScopedToTargetUID(t *testing.T) {
	for _, uid := range []types.UID{"created-uid", "replacement-uid", ""} {
		for _, skipped := range []bool{false, true} {
			t.Run(fmt.Sprintf("uid=%q/skipped=%t", uid, skipped), func(t *testing.T) {
				obj := policyConfigMap("tenant-a", "target")
				attempts := 0
				policy := &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Condition: "object == null || !has(object.metadata.annotations)"}
				c := fake.NewClientBuilder().WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						obj.SetUID("created-uid")
						obj.SetCreationTimestamp(metav1.Now())
						return c.Create(ctx, obj, opts...)
					},
					Patch: func(ctx context.Context, c client.WithWatch, target client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if patch.Type() != types.ApplyPatchType {
							return c.Patch(ctx, target, patch, opts...)
						}
						attempts++
						if attempts == 1 {
							if uid != "created-uid" {
								require.NoError(t, c.Delete(ctx, obj))
								replacement := policyConfigMap(obj.GetNamespace(), obj.GetName())
								replacement.SetUID(uid)
								require.NoError(t, c.Create(ctx, replacement))
							}
							if skipped {
								current := obj.DeepCopy()
								require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), current))
								current.SetAnnotations(map[string]string{"skip": "true"})
								require.NoError(t, c.Update(ctx, current))
							}
						}
						return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent writer"))
					},
				}).Build()
				conditions, err := cache.NewCELCache()
				require.NoError(t, err)
				result, err := (&Processor{Conditions: conditions}).Apply(t.Context(), c, obj, "replication/test", false, false, nil, nil, policy)
				if skipped {
					require.NoError(t, err)
					require.True(t, result.Skipped)
				} else {
					require.True(t, apierrors.IsConflict(err))
				}
				require.Equal(t, uid, result.UID)
				if uid == "created-uid" {
					require.NotNil(t, result.LastApply)
					require.False(t, result.LastApply.IsZero())
				} else {
					require.Nil(t, result.LastApply, "must not transfer a successful write to another or unknown UID")
					require.False(t, result.Created)
				}
			})
		}
	}
}
