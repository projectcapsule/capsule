// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"errors"
	"testing"

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
