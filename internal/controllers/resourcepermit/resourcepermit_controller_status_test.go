// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestResourcePermitReconcilePreservesConcurrentExpiration(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"before finalizer read", "after finalizer read", "during status write"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			scheme := runtime.NewScheme()
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			permit := &capsulev1beta2.ResourcePermit{
				Name: "permit", Namespace: "tenant-a", UID: "permit-uid",
				Finalizers: []string{meta.ControllerFinalizer},
				Status: capsulev1beta2.ResourcePermitStatus{
					Phase: capsulev1beta2.ResourcePermitPhaseActive,
				},
			}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(permit).WithStatusSubresource(permit).Build()
			key := client.ObjectKeyFromObject(permit)
			stale := &capsulev1beta2.ResourcePermit{}
			require.NoError(t, base.Get(ctx, key, stale))
			var expiredStatus capsulev1beta2.ResourcePermitStatus
			expire := func() {
				current := &capsulev1beta2.ResourcePermit{}
				require.NoError(t, base.Get(ctx, key, current))
				require.NoError(t, current.ExpirePermit(nil))
				require.NoError(t, base.Status().Update(ctx, current))
				expiredStatus = *current.Status.DeepCopy()
			}
			reads, writes := 0, 0
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					if stage == "before finalizer read" && reads == 1 {
						expire()
					}
					err := c.Get(ctx, key, obj, opts...)
					if stage == "after finalizer read" && reads == 1 {
						expire()
					}
					return err
				},
				SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					if stage == "during status write" && writes == 1 {
						expire()
					}
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				},
			})
			r := &ResourcePermitReconciler{Client: cl}
			_, err := r.reconcile(ctx, logr.Discard(), stale)
			current := &capsulev1beta2.ResourcePermit{}
			require.NoError(t, base.Get(ctx, key, current))
			require.Equal(t, expiredStatus, current.Status, "an accepted expiration and its audit record must survive a stale reconcile")
			require.True(t, apierrors.IsConflict(err), "a changed permit must be reconciled again, got %v", err)
			require.LessOrEqual(t, writes, 1, "do not retry a stale status against a newer resourceVersion")
			_, err = r.reconcile(ctx, logr.Discard(), current)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(base.Get(ctx, key, current)), "a fresh reconcile must finish expiration")
		})
	}
}

func TestResourcePermitStatusUpdateAdvancesVersion(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a", UID: "permit-uid"}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(permit).WithStatusSubresource(permit).Build()
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(permit), permit))
	r := &ResourcePermitReconciler{Client: cl}
	for _, phase := range []capsulev1beta2.ResourcePermitPhase{capsulev1beta2.ResourcePermitPhaseCreated, capsulev1beta2.ResourcePermitPhaseRequested} {
		before := permit.ResourceVersion
		permit.Status.Phase = phase
		require.NoError(t, r.updateStatus(t.Context(), logr.Discard(), permit))
		require.NotEqual(t, before, permit.ResourceVersion, "successive status writes in one reconcile need the accepted version")
		current := &capsulev1beta2.ResourcePermit{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(permit), current))
		require.Equal(t, permit.Status, current.Status)
		require.Equal(t, permit.ResourceVersion, current.ResourceVersion)
	}
}

func TestResourcePermitStatusUpdatesIgnoreLaggingCache(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a", UID: "permit-uid"}
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(permit).WithStatusSubresource(permit).Build()
	require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(permit), permit))
	cacheSnapshot := permit.DeepCopy()
	reads := 0
	cl := interceptor.NewClient(api, interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			reads++
			cacheSnapshot.DeepCopyInto(obj.(*capsulev1beta2.ResourcePermit))
			return nil
		},
	})
	r := &ResourcePermitReconciler{Client: cl}
	for _, phase := range []capsulev1beta2.ResourcePermitPhase{capsulev1beta2.ResourcePermitPhaseCreated, capsulev1beta2.ResourcePermitPhaseRequested} {
		permit.Status.Phase = phase
		require.NoError(t, r.updateStatus(t.Context(), logr.Discard(), permit))
		current := &capsulev1beta2.ResourcePermit{}
		require.NoError(t, api.Get(t.Context(), client.ObjectKeyFromObject(permit), current))
		require.Equal(t, phase, current.Status.Phase)
		require.Equal(t, current.ResourceVersion, permit.ResourceVersion)
	}
	require.Zero(t, reads, "avoid both API reads and false conflicts from stale informer reads")
}

func TestResourcePermitStatusUpdatePreservesOtherChanges(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"metadata changed", "recreated", "gone", "cache unavailable", "write failure"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			scheme := runtime.NewScheme()
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: "tenant-a", UID: "original"}
			other := &capsulev1beta2.ResourcePermit{Name: permit.Name, Namespace: "tenant-b", UID: "other-tenant"}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(permit, other).WithStatusSubresource(permit).Build()
			key := client.ObjectKeyFromObject(permit)
			require.NoError(t, base.Get(ctx, key, permit))
			snapshot := permit.DeepCopy()
			switch state {
			case "metadata changed":
				permit.Labels = map[string]string{"updated": "true"}
				require.NoError(t, base.Update(ctx, permit))
			case "recreated":
				require.NoError(t, base.Delete(ctx, permit))
				permit = &capsulev1beta2.ResourcePermit{Name: permit.Name, Namespace: permit.Namespace, UID: "replacement"}
				require.NoError(t, base.Create(ctx, permit))
				// The fake reuses versions on CREATE. Model the API server's distinct
				// replacement version without adopting it into the stale snapshot.
				require.NoError(t, base.Update(ctx, permit))
			case "gone":
				require.NoError(t, base.Delete(ctx, permit))
			}
			dependencyErr := errors.New("API unavailable")
			writes := 0
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if state == "cache unavailable" {
						return dependencyErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
				SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					if state == "write failure" {
						return dependencyErr
					}
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				},
			})
			snapshot.Status.Phase = capsulev1beta2.ResourcePermitPhaseActive
			r := &ResourcePermitReconciler{Client: cl}
			err := r.updateStatus(ctx, logr.Discard(), snapshot)
			switch state {
			case "metadata changed", "recreated":
				require.True(t, apierrors.IsConflict(err), "got %v", err)
			case "gone":
				require.NoError(t, err)
			case "cache unavailable":
				require.NoError(t, err, "status writes must not depend on the informer cache")
			case "write failure":
				require.ErrorIs(t, err, dependencyErr)
			}
			require.Equal(t, 1, writes, "send the original version once; never retry a stale decision")
			if state != "gone" {
				current := &capsulev1beta2.ResourcePermit{}
				require.NoError(t, base.Get(ctx, key, current))
				if state == "cache unavailable" {
					require.Equal(t, snapshot.Status, current.Status)
					require.Equal(t, permit.UID, current.UID)
				} else {
					require.Equal(t, permit, current)
				}
			}
			currentOther := &capsulev1beta2.ResourcePermit{}
			require.NoError(t, base.Get(ctx, client.ObjectKeyFromObject(other), currentOther))
			require.Empty(t, currentOther.Status.Phase, "status must remain scoped to its own namespace")
		})
	}
}
