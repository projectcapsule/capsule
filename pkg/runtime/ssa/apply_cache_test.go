// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type applyCacheTestClient struct{ client.WithWatch }

type identifiedApplyTestClient struct {
	client.WithWatch
	identity string
}

func (c *identifiedApplyTestClient) ApplyCacheIdentity() any { return c.identity }

func applyCacheFixture(t testing.TB) (Manager, *applyCacheTestClient, *unstructured.Unstructured, ApplyOptions, *int, *bool) {
	t.Helper()
	obj := configMap("cached", map[string]any{"value": "desired"})
	obj.SetUID("target-uid")
	patches := new(int)
	fail := new(bool)
	c := &applyCacheTestClient{fake.NewClientBuilder().WithObjects(obj.DeepCopy()).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			*patches++
			if *fail {
				return apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent change"))
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()}
	m := skippedPolicyManager(t)
	m.Applied = cache.NewAppliedResources[ApplyCacheKey, ApplyResult](func(r ApplyResult) ApplyResult { r.LastApply = r.LastApply.DeepCopy(); return r })
	opts := ApplyOptions{FieldOwner: testFieldOwner, Adopt: true, Force: true, Protect: true}
	return m, c, obj, opts, patches, fail
}

func TestApplyCacheSkipsOnlyVerifiedUnchangedTargets(t *testing.T) {
	for _, change := range []string{"unchanged", "intent", "drift", "replacement", "policy", "identity", "dry-run", "read-denied", "condition", "metadata"} {
		t.Run(change, func(t *testing.T) {
			m, c, obj, opts, patches, _ := applyCacheFixture(t)
			original := obj.DeepCopy()
			first, err := m.Apply(t.Context(), c, obj, opts)
			require.NoError(t, err)
			require.Positive(t, *patches)
			require.Equal(t, original, obj, "input belongs to the caller")
			before := *patches
			var applyClient client.Client = c
			switch change {
			case "intent":
				require.NoError(t, unstructured.SetNestedField(obj.Object, "changed", "data", "value"))
			case "drift", "replacement", "metadata":
				live := obj.DeepCopy()
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
				if change == "drift" {
					require.NoError(t, unstructured.SetNestedField(live.Object, "drift", "data", "value"))
				}
				if change == "metadata" {
					live.SetAnnotations(map[string]string{"external": "changed"})
				}
				if change == "replacement" {
					require.NoError(t, c.Delete(t.Context(), live))
					live.SetResourceVersion("")
					live.SetUID("replacement")
					require.NoError(t, c.Create(t.Context(), live))
				} else {
					require.NoError(t, c.Update(t.Context(), live))
				}
			case "policy":
				opts.Protect = false
			case "identity":
				applyClient = interceptor.NewClient(c, interceptor.Funcs{})
			case "dry-run":
				opts.DryRun = true
			case "read-denied":
				applyClient = interceptor.NewClient(c, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return apierrors.NewForbidden(corev1.Resource("configmaps"), obj.GetName(), nil)
				}})
			case "condition":
				opts.Condition = "false"
			}
			second, err := m.Apply(t.Context(), applyClient, obj, opts)
			if change == "read-denied" {
				require.True(t, apierrors.IsForbidden(err))
				require.Equal(t, before, *patches)
				return
			}
			require.NoError(t, err)
			if change == "unchanged" {
				require.Equal(t, before, *patches, "unchanged target must not be patched")
				require.False(t, second.ContentChanged)
				require.Equal(t, first.UID, second.UID)
				require.Equal(t, first.ContentVersion, second.ContentVersion)
				require.Equal(t, first.LastApply, second.LastApply)
			} else if change == "condition" {
				require.True(t, second.Skipped)
			} else {
				require.Greater(t, *patches, before, "changed inputs must execute SSA")
			}
		})
	}
}

func TestApplyCacheDoesNotRememberFailedWrite(t *testing.T) {
	m, c, obj, opts, patches, fail := applyCacheFixture(t)
	_, err := m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(obj.Object, "new", "data", "value"))
	*fail = true
	_, err = m.Apply(t.Context(), c, obj, opts)
	require.True(t, apierrors.IsConflict(err))
	before := *patches
	*fail = false
	_, err = m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	require.Greater(t, *patches, before)
}

func TestApplyCacheRejectsDifferentOwnerReference(t *testing.T) {
	m, c, obj, opts, _, _ := applyCacheFixture(t)
	opts.OwnerReference = &metav1.OwnerReference{APIVersion: "v1", Kind: "Namespace", Name: "first", UID: types.UID("first")}
	first, ok := m.applyCacheKey(c, obj, opts)
	require.True(t, ok)
	opts.OwnerReference.UID = "replacement"
	second, ok := m.applyCacheKey(c, obj, opts)
	require.True(t, ok)
	require.NotEqual(t, first, second)
}

func TestApplyCacheSharesVerifiedCredentialLifetime(t *testing.T) {
	m, base, obj, opts, patches, _ := applyCacheFixture(t)
	firstClient := &identifiedApplyTestClient{WithWatch: base, identity: "one-credential-lifetime"}
	_, err := m.Apply(t.Context(), firstClient, obj, opts)
	require.NoError(t, err)
	before := *patches
	secondClient := &identifiedApplyTestClient{WithWatch: base, identity: "one-credential-lifetime"}
	_, err = m.Apply(t.Context(), secondClient, obj, opts)
	require.NoError(t, err)
	require.Equal(t, before, *patches)
	secondClient.identity = "rotated-credentials"
	_, err = m.Apply(t.Context(), secondClient, obj, opts)
	require.NoError(t, err)
	require.Greater(t, *patches, before)
}

func TestApplyCacheUsesVerifiedCreationProvenance(t *testing.T) {
	m, c, obj, opts, patches, _ := applyCacheFixture(t)
	require.NoError(t, c.Delete(t.Context(), obj))
	// Fake Create does not assign a UID, unlike a real API server.
	c.WithWatch = interceptor.NewClient(c.WithWatch, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		obj.SetUID("created-uid")
		return c.Create(ctx, obj, opts...)
	}})
	first, err := m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	require.True(t, first.Created)
	before := *patches
	opts.PreviouslyCreated = true
	second, err := m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	require.True(t, second.Created)
	require.Equal(t, before, *patches, "recording creation in inventory must not trigger another apply")
}

func TestReplicationManagerSurvivesCompleteContentDrift(t *testing.T) {
	m, c, obj, opts, _, _ := applyCacheFixture(t)
	m.Metadata.ManagedByValue = meta.ValueControllerReplications
	opts.Protect = false
	_, err := m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	live := obj.DeepCopy()
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
	require.NoError(t, unstructured.SetNestedField(live.Object, "drift", "data", "value"))
	require.NoError(t, c.Update(t.Context(), live, client.FieldOwner("tenant-owner")))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
	require.True(t, hasFieldManager(live, opts.FieldOwner), "tracking ownership must survive user updates to every declared data field")
	_, err = m.Apply(t.Context(), c, obj, opts)
	require.NoError(t, err)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
	value, _, err := unstructured.NestedString(live.Object, "data", "value")
	require.NoError(t, err)
	require.Equal(t, "desired", value)
}

func BenchmarkVerifiedApply(b *testing.B) {
	for _, protected := range []bool{false, true} {
		b.Run(fmt.Sprintf("protected=%t", protected), func(b *testing.B) {
			benchmarkVerifiedApply(b, protected)
		})
	}
}

func benchmarkVerifiedApply(b *testing.B, protected bool) {
	for _, cached := range []bool{false, true} {
		name := "ssa"
		if cached {
			name = "verified-noop"
		}
		b.Run(name, func(b *testing.B) {
			m, c, obj, opts, patches, _ := applyCacheFixture(b)
			m.Metadata.ManagedByValue = meta.ValueControllerReplications
			opts.Protect = protected
			if !cached {
				m.Applied = nil
			}
			_, err := m.Apply(b.Context(), c, obj, opts)
			require.NoError(b, err)
			before := *patches
			b.ReportAllocs()
			for b.Loop() {
				_, err := m.Apply(b.Context(), c, obj, opts)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(*patches-before)/float64(b.N), "patches/op")
		})
	}
}
