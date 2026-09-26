// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestCleanupIgnoresUnownedReplacement(t *testing.T) {
	for _, cleanup := range []string{"remove", "orphan", "disown"} {
		t.Run(cleanup, func(t *testing.T) {
			obj := configMap("replacement", map[string]any{"foreign": "retained"})
			obj.SetUID("replacement-uid")
			obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy, meta.NewManagedByCapsuleLabel: testCreatedBy, meta.ProtectedByCapsuleLabel: testCreatedBy})
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{managedField("external")})
			c := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "default"}, obj).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					t.Fatal("patched unowned replacement")
					return nil
				},
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					t.Fatal("deleted unowned replacement")
					return nil
				},
			}).Build()
			m := skippedPolicyManager(t)
			var err error
			switch cleanup {
			case "remove":
				_, err = m.Prune(t.Context(), c, obj, PruneOptions{FieldOwner: testFieldOwner, PreviouslyCreated: true})
			case "orphan":
				err = m.Orphan(t.Context(), c, obj, testFieldOwner, nil)
			case "disown":
				err = m.Disown(t.Context(), c, obj, testFieldOwner, nil)
			}
			require.NoError(t, err)
		})
	}
}

func TestCreationProvenanceSurvivesMetadataAndStatusFailure(t *testing.T) {
	base := fake.NewClientBuilder().WithReturnManagedFields().Build()
	fail := true
	c := interceptor.NewClient(base, interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if fail && patch.Type() == types.JSONPatchType {
			return errors.New("metadata unavailable")
		}
		return c.Patch(ctx, obj, patch, opts...)
	}})
	m := skippedPolicyManager(t)
	obj := configMap("interrupted", map[string]any{"managed": "value"})
	result, err := m.Apply(t.Context(), c, obj, ApplyOptions{FieldOwner: testFieldOwner})
	require.ErrorContains(t, err, "metadata unavailable")
	require.True(t, result.Created)
	fail = false
	result, err = m.Apply(t.Context(), c, obj, ApplyOptions{FieldOwner: testFieldOwner})
	require.NoError(t, err)
	require.True(t, result.Created)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.Equal(t, testCreatedBy, obj.GetLabels()[meta.CreatedByCapsuleLabel])
}

func TestCleanupUsesLiveVersionPreconditions(t *testing.T) {
	for _, created := range []bool{false, true} {
		t.Run(map[bool]string{false: "prune", true: "delete"}[created], func(t *testing.T) {
			obj := configMap("racing", map[string]any{"value": "managed"})
			obj.SetUID("original-uid")
			obj.SetResourceVersion("7")
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{managedField(testFieldOwner)})
			base := fake.NewClientBuilder().WithObjects(&corev1.Namespace{Name: "default"}, obj).WithReturnManagedFields().Build()
			writes := 0
			c := interceptor.NewClient(base, interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, actual client.Object, opts ...client.DeleteOption) error {
					writes++
					options := (&client.DeleteOptions{}).ApplyOptions(opts)
					require.Equal(t, obj.GetUID(), *options.Preconditions.UID)
					require.Equal(t, "7", *options.Preconditions.ResourceVersion)
					return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("replaced"))
				},
				Patch: func(ctx context.Context, c client.WithWatch, actual client.Object, patch client.Patch, opts ...client.PatchOption) error {
					writes++
					require.Equal(t, types.ApplyPatchType, patch.Type())
					require.Equal(t, "7", actual.GetResourceVersion())
					require.Equal(t, obj.GetUID(), actual.GetUID())
					return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("replaced"))
				},
			})
			_, err := (Manager{}).Prune(t.Context(), c, obj, PruneOptions{FieldOwner: testFieldOwner, PreviouslyCreated: created})
			require.True(t, apierrors.IsConflict(err))
			require.Equal(t, 1, writes)
		})
	}
}
