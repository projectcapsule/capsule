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
	// Legacy non-pruning cleanup can retain the departing manager's fields.
	existing := skippedPolicyTarget(testFieldOwner, true)
	labels := existing.GetLabels()
	labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
	existing.SetLabels(labels)
	fields := existing.GetManagedFields()
	fields = append(fields, managedField("external"), managedField(meta.ResourceControllerFieldOwnerPrefix()))
	existing.SetManagedFields(fields)
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
	m := skippedPolicyManager(t)
	require.NoError(t, m.Disown(t.Context(), c, existing, testFieldOwner, nil))
	actual := configMap("guarded", nil)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	require.NotContains(t, actual.GetLabels(), meta.NewManagedByCapsuleLabel)
	require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
	require.NotContains(t, actual.GetAnnotations(), meta.ResourcePermitServiceAccountAnnotation)
	require.Equal(t, existing.Object["data"], actual.Object["data"])
	require.Equal(t, "original", actual.GetLabels()["example.org/keep"])
	require.Equal(t, "original", actual.GetAnnotations()["example.org/keep"])
}

func TestDisownPreservesSharedProtection(t *testing.T) {
	for _, operation := range []metav1.ManagedFieldsOperationType{metav1.ManagedFieldsOperationApply, metav1.ManagedFieldsOperationUpdate} {
		t.Run(string(operation), func(t *testing.T) {
			const remaining = "2lclct9cwq6mg/default/tenant-a/0/raw-0/"
			existing := skippedPolicyTarget(remaining, true)
			fields := existing.GetManagedFields()
			fields[0].Operation = operation
			existing.SetManagedFields(fields)
			labels := existing.GetLabels()
			labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
			existing.SetLabels(labels)
			c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
			m := skippedPolicyManager(t)
			m.ReplicationOwners = knownReplicationOwners(remaining)
			require.NoError(t, m.Disown(t.Context(), c, existing, testFieldOwner, nil))
			actual := configMap("guarded", nil)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			require.Equal(t, existing.GetLabels(), actual.GetLabels())
			require.Equal(t, existing.GetAnnotations(), actual.GetAnnotations())
			require.Equal(t, existing.Object["data"], actual.Object["data"])
			// Once the remaining resource owner departs, external fields must not
			// keep the lifecycle metadata around indefinitely.
			require.NoError(t, m.Disown(t.Context(), c, actual, remaining, nil))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			require.NotContains(t, actual.GetLabels(), meta.NewManagedByCapsuleLabel)
			require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
			require.NotContains(t, actual.GetAnnotations(), meta.ResourcePermitServiceAccountAnnotation)
		})
	}
}

func TestApplyRemovesProtectionFromPreviousSkip(t *testing.T) {
	for _, condition := range []string{"true", ""} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("condition=%q/shared=%t", condition, shared), func(t *testing.T) {
				existing := skippedPolicyTarget(testFieldOwner, false)
				if shared {
					fields := existing.GetManagedFields()
					other := fields[0].DeepCopy()
					other.Manager = "2lclct9cwq6mg/default/tenant-a/0/raw-0/"
					existing.SetManagedFields(append(fields, *other))
				}
				c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
				m := skippedPolicyManager(t)
				m.ReplicationOwners = knownReplicationOwners("2lclct9cwq6mg/default/tenant-a/0/raw-0/")
				opts := ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Protect: true, Adopt: true}
				result, err := m.Apply(t.Context(), c, existing, opts)
				require.NoError(t, err)
				require.True(t, result.Skipped)
				opts.Condition, opts.Protect = condition, false
				desired := configMap("guarded", map[string]any{"value": "retained", "new": "applied"})
				result, err = m.Apply(t.Context(), c, desired, opts)
				require.NoError(t, err)
				require.False(t, result.Skipped)
				actual := configMap("guarded", nil)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
				require.Equal(t, desired.Object["data"], actual.Object["data"])
				if shared {
					require.Equal(t, testCreatedBy, actual.GetLabels()[meta.ProtectedByCapsuleLabel])
					require.Equal(t, m.Metadata.ProtectedByServiceAccount, actual.GetAnnotations()[meta.ResourcePermitServiceAccountAnnotation])
				} else {
					require.NotContains(t, actual.GetLabels(), meta.ProtectedByCapsuleLabel)
					require.NotContains(t, actual.GetAnnotations(), meta.ResourcePermitServiceAccountAnnotation)
				}
			})
		}
	}
}

func TestSkippedPolicyPreservesSharedProtection(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, true)
	fields := existing.GetManagedFields()
	other := fields[0].DeepCopy()
	other.Manager = meta.ResourceFieldOwner("remaining")
	existing.SetManagedFields(append(fields, *other))
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
	m := skippedPolicyManager(t)
	result, err := m.Apply(t.Context(), c, existing, ApplyOptions{FieldOwner: testFieldOwner, Condition: "false", Protect: false, Adopt: true})
	require.NoError(t, err)
	require.True(t, result.Skipped)
	actual := configMap("guarded", nil)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
	require.Equal(t, existing.GetLabels(), actual.GetLabels())
	require.Equal(t, existing.GetAnnotations(), actual.GetAnnotations())
}

func TestApplyTracksSharedFieldsWithoutManagerTimestamp(t *testing.T) {
	existing := skippedPolicyTarget(testFieldOwner, false)
	existing.SetLabels(nil) // An adopted target, not created by Capsule.
	c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if err := c.Patch(ctx, obj, patch, opts...); err != nil {
				return err
			}
			fields := obj.GetManagedFields()
			for i := range fields {
				fields[i].Time = nil
			}
			obj.SetManagedFields(fields)
			return nil
		},
	}).Build()
	m := skippedPolicyManager(t)
	before := metav1.Now()
	result, err := m.Apply(t.Context(), c, configMap("guarded", map[string]any{"value": "retained"}), ApplyOptions{FieldOwner: testFieldOwner, Adopt: true})
	require.NoError(t, err)
	require.False(t, result.Created)
	require.NotNil(t, result.LastApply, "a shared SSA claim is still a completed apply that requires cleanup")
	require.False(t, result.LastApply.Before(&before))
}

func TestOrphanPreservesSharedProtection(t *testing.T) {
	for _, peer := range []string{meta.ResourceFieldOwner("remaining"), "2lclct9cwq6mg/default/tenant-a/0/raw-0/"} {
		t.Run(peer, func(t *testing.T) {
			existing := skippedPolicyTarget(testFieldOwner, true)
			labels := existing.GetLabels()
			labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
			existing.SetLabels(labels)
			fields := existing.GetManagedFields()
			remaining := fields[0].DeepCopy()
			remaining.Manager = peer
			existing.SetManagedFields(append(fields, *remaining))
			c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
			m := skippedPolicyManager(t)
			m.ReplicationOwners = knownReplicationOwners(peer)
			require.NoError(t, m.Orphan(t.Context(), c, existing, testFieldOwner, nil))
			actual := configMap("guarded", nil)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			require.Equal(t, existing.GetLabels(), actual.GetLabels())
			require.Equal(t, existing.GetAnnotations(), actual.GetAnnotations())
			require.Equal(t, existing.Object["data"], actual.Object["data"])
			require.True(t, hasFieldManager(actual, peer))
		})
	}
}

func TestProtectionCleanupChecksResourceVersion(t *testing.T) {
	for _, operation := range []string{"apply", "disown", "orphan"} {
		t.Run(operation, func(t *testing.T) {
			existing := skippedPolicyTarget(testFieldOwner, true)
			labels := existing.GetLabels()
			labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
			existing.SetLabels(labels)
			writes := 0
			c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if patch.Type() == types.JSONPatchType {
						writes++
						current := configMap("guarded", nil)
						require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), current))
						current.SetManagedFields(append(current.GetManagedFields(), managedField(meta.ResourceFieldOwner("concurrent"))))
						current.SetAnnotations(map[string]string{"concurrent": "retained", meta.ResourcePermitServiceAccountAnnotation: "another-owner"})
						require.NoError(t, c.Update(ctx, current))
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			m := skippedPolicyManager(t)
			var err error
			if operation == "disown" {
				err = m.Disown(t.Context(), c, existing, testFieldOwner, nil)
			} else if operation == "orphan" {
				err = m.Orphan(t.Context(), c, existing, testFieldOwner, nil)
			} else {
				_, err = m.Apply(t.Context(), c, configMap("guarded", map[string]any{"value": "retained"}), ApplyOptions{FieldOwner: testFieldOwner, Condition: "true", Adopt: true})
			}
			require.Error(t, err)
			require.Equal(t, 1, writes, "must re-read ownership instead of retrying stale cleanup")
			actual := configMap("guarded", nil)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
			require.Equal(t, testCreatedBy, actual.GetLabels()[meta.ProtectedByCapsuleLabel])
			require.Equal(t, "another-owner", actual.GetAnnotations()[meta.ResourcePermitServiceAccountAnnotation])
		})
	}
}

func BenchmarkProtectionCleanup(b *testing.B) {
	for _, operation := range []string{"apply", "disown", "orphan"} {
		for _, peers := range []int{0, 16} {
			b.Run(fmt.Sprintf("%s/peers=%d", operation, peers), func(b *testing.B) {
				m := skippedPolicyManager(b)
				known := make([]string, 0, peers)
				for i := range peers {
					known = append(known, fmt.Sprintf("%d/default/tenant-a/0/raw-0/", i+1))
				}
				m.ReplicationOwners = knownReplicationOwners(known...)
				// Warm the shared condition compiler outside the timed operation.
				warmClient := fake.NewClientBuilder().Build()
				_, err := m.Apply(b.Context(), warmClient, configMap("warm", nil), ApplyOptions{FieldOwner: testFieldOwner, Condition: "false"})
				if err != nil {
					b.Fatal(err)
				}
				reads, writes := 0, 0
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					existing := skippedPolicyTarget(testFieldOwner, true)
					labels := existing.GetLabels()
					labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
					existing.SetLabels(labels)
					fields := existing.GetManagedFields()
					for i := range peers {
						field := fields[0].DeepCopy()
						field.Manager = fmt.Sprintf("%d/default/tenant-a/0/raw-0/", i+1)
						fields = append(fields, *field)
					}
					existing.SetManagedFields(fields)
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
					desired := configMap("guarded", map[string]any{"value": "retained", "new": "applied"})
					b.StartTimer()
					if operation == "disown" {
						err = m.Disown(b.Context(), c, existing, testFieldOwner, nil)
					} else if operation == "orphan" {
						err = m.Orphan(b.Context(), c, existing, testFieldOwner, nil)
					} else {
						_, err = m.Apply(b.Context(), c, desired, ApplyOptions{FieldOwner: testFieldOwner, Condition: "true", Adopt: true})
					}
					if err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					actual := configMap("guarded", nil)
					if err := c.Get(b.Context(), client.ObjectKeyFromObject(existing), actual); err != nil {
						b.Fatal(err)
					}
					reads-- // Exclude validation reads from the operation's metrics.
					if protected := actual.GetLabels()[meta.ProtectedByCapsuleLabel] == testCreatedBy; protected != (peers > 0) {
						b.Fatal("unexpected protection after cleanup")
					}
					b.StartTimer()
				}
				b.ReportMetric(float64(reads)/float64(b.N), "reads/op")
				b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
			})
		}
	}
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
