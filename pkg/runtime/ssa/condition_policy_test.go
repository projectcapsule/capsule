// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func skippedPolicyTarget(owner string, protected bool) *unstructured.Unstructured {
	obj := configMap("guarded", map[string]any{"value": "retained"})
	obj.SetLabels(map[string]string{meta.CreatedByCapsuleLabel: testCreatedBy, "example.org/keep": "original"})
	obj.SetAnnotations(map[string]string{"example.org/keep": "original"})
	if protected {
		labels := obj.GetLabels()
		labels[meta.ProtectedByCapsuleLabel] = testCreatedBy
		obj.SetLabels(labels)
		annotations := obj.GetAnnotations()
		annotations[meta.ResourcePermitServiceAccountAnnotation] = "system:serviceaccount:test:old"
		obj.SetAnnotations(annotations)
	}
	field := managedField(owner)
	field.FieldsV1 = &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:value":{}}}`)}
	field.Time = &metav1.Time{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{field})
	return obj
}

func skippedPolicyManager(t testing.TB) Manager {
	t.Helper()
	conditions, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	return Manager{Conditions: conditions, Metadata: Metadata{
		CreatedByValue: testCreatedBy, ManagedByValue: testCreatedBy, ProtectedByValue: testCreatedBy,
		ProtectedByServiceAccountAnnotation: meta.ResourcePermitServiceAccountAnnotation,
		ProtectedByServiceAccount:           "system:serviceaccount:test:runner",
	}}
}

func TestSkippedConditionReconcilesProtection(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		protected, protect, foreign, dryRun bool
	}{
		{name: "enable", protect: true},
		{name: "disable", protected: true},
		{name: "refresh execution identity", protected: true, protect: true},
		{name: "foreign with Capsule labels", protected: true, foreign: true},
		{name: "dry run", protected: true, dryRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := testFieldOwner
			if tc.foreign {
				owner = "other-parent"
			}
			existing := skippedPolicyTarget(owner, tc.protected)
			c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
			m := skippedPolicyManager(t)
			desired := configMap("guarded", map[string]any{"value": "must-not-apply"})
			desired.SetAnnotations(map[string]string{"example.org/keep": "must-not-apply"})
			result, err := m.Apply(t.Context(), c, desired, ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Protect: tc.protect, Adopt: true, DryRun: tc.dryRun})
			require.NoError(t, err)
			require.True(t, result.Skipped)
			actual := configMap("guarded", nil)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			require.Equal(t, existing.Object["data"], actual.Object["data"])
			require.Equal(t, "original", actual.GetLabels()["example.org/keep"])
			require.Equal(t, "original", actual.GetAnnotations()["example.org/keep"])
			if tc.foreign || tc.dryRun {
				require.Equal(t, existing.GetLabels(), actual.GetLabels())
				require.Equal(t, existing.GetAnnotations(), actual.GetAnnotations())
			} else if tc.protect {
				require.Equal(t, testCreatedBy, actual.GetLabels()[meta.ProtectedByCapsuleLabel])
				require.Equal(t, m.Metadata.ProtectedByServiceAccount, actual.GetAnnotations()[meta.ResourcePermitServiceAccountAnnotation])
			} else {
				require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
				require.NotContains(t, actual.GetAnnotations(), meta.ResourcePermitServiceAccountAnnotation)
			}
			if !tc.foreign {
				require.True(t, result.LastApply.Equal(existing.GetManagedFields()[0].Time))
			}
		})
	}
}

func TestSkippedConditionPolicyPatchFailure(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, false)
	writes := 0
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			writes++
			return errors.New("policy write denied")
		},
	}).Build()
	m := skippedPolicyManager(t)
	result, err := m.Apply(t.Context(), c, existing, ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Protect: true})
	require.ErrorContains(t, err, "policy write denied")
	require.True(t, result.Skipped)
	require.Equal(t, 1, writes)
}

func TestSkippedConditionKeepsAdoptedLifecycleOnCreationPolicyChange(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, false)
	labels := existing.GetLabels()
	delete(labels, meta.CreatedByCapsuleLabel)
	existing.SetLabels(labels)
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
	m := skippedPolicyManager(t)
	// Disabling adoption must not turn a previously adopted target into one that
	// Capsule created. Cleanup must still relinquish fields instead of deleting it.
	result, err := m.Apply(t.Context(), c, existing, ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Adopt: false, Protect: true})
	require.NoError(t, err)
	require.True(t, result.Skipped)
	require.True(t, result.PolicyReconciled)
	require.False(t, result.Created)
	require.True(t, result.LastApply.Equal(existing.GetManagedFields()[0].Time))
}

func TestSkippedConditionPolicyChecksResourceVersion(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, false)
	writes := 0
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			writes++
			require.Equal(t, types.JSONPatchType, patch.Type())
			current := configMap("guarded", nil)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), current))
			current.SetLabels(map[string]string{"concurrent": "retained"})
			require.NoError(t, c.Update(ctx, current))
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	m := skippedPolicyManager(t)
	_, err := m.Apply(t.Context(), c, existing, ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Protect: true})
	require.Error(t, err)
	require.Equal(t, 1, writes, "must re-read on the next reconciliation, not retry a stale patch")
	actual := configMap("guarded", nil)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	require.Equal(t, map[string]string{"concurrent": "retained"}, actual.GetLabels())
}

func TestDisownRemovesPolicyProtection(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, true)
	labels := existing.GetLabels()
	labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
	existing.SetLabels(labels)
	c := fake.NewClientBuilder().WithObjects(existing).Build()
	m := skippedPolicyManager(t)
	require.NoError(t, m.Disown(t.Context(), c, existing, nil))
	actual := configMap("guarded", nil)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	require.NotContains(t, actual.GetLabels(), meta.NewManagedByCapsuleLabel)
	require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
	require.NotContains(t, actual.GetAnnotations(), meta.ResourcePermitServiceAccountAnnotation)
	require.Equal(t, existing.Object["data"], actual.Object["data"])
	require.Equal(t, "original", actual.GetLabels()["example.org/keep"])
	require.Equal(t, "original", actual.GetAnnotations()["example.org/keep"])
}

func BenchmarkSkippedConditionPolicy(b *testing.B) {
	for _, owned := range []bool{false, true} {
		for _, changing := range []bool{false, true} {
			b.Run(fmt.Sprintf("owned=%t/changing=%t", owned, changing), func(b *testing.B) {
				owner := "another-parent"
				if owned {
					owner = testFieldOwner
				}
				existing := skippedPolicyTarget(owner, false)
				reads, writes := 0, 0
				c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						return c.Get(ctx, key, obj, opts...)
					},
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						writes++
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).Build()
				m := skippedPolicyManager(b)
				opts := ApplyOptions{FieldOwner: testFieldOwner, Condition: "false"}
				if _, err := m.Apply(b.Context(), c, existing, opts); err != nil {
					b.Fatal(err)
				}
				reads, writes = 0, 0
				b.ReportAllocs()
				for b.Loop() {
					if changing {
						opts.Protect = !opts.Protect
					}
					if _, err := m.Apply(b.Context(), c, existing, opts); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(reads)/float64(b.N), "reads/op")
				b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
				if reads != b.N || (owned && changing && writes != b.N) || ((!owned || !changing) && writes != 0) {
					b.Fatalf("unexpected API calls: reads=%d writes=%d iterations=%d", reads, writes, b.N)
				}
			})
		}
	}
}
