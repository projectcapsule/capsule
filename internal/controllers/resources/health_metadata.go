// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"encoding/json"
	"slices"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Apply, health and namespace queues share status. After the status update, patch
// only owned metadata so a second status merge cannot undo a newer observation.
func patchReplicationMetadata(ctx context.Context, c client.Client, reader client.Reader, obj client.Object, originalFinalizers []string, manualRequest string) error {
	if slices.Equal(originalFinalizers, obj.GetFinalizers()) && manualRequest == "" {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		fresh := obj.DeepCopyObject().(client.Object) //nolint:forcetypeassert // Kubernetes objects implement client.Object.
		if err := reader.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
			return client.IgnoreNotFound(err)
		}

		if fresh.GetUID() != obj.GetUID() {
			return nil
		}

		// A scoped writer can publish cleanup records after the full status
		// update. Recheck on every optimistic-lock attempt before retiring their
		// finalizer; the full writer's in-memory inventory may already be stale.
		_, status := healthSource(fresh)
		retainCleanup := status != nil && len(status.ProcessedItems) > 0

		for _, finalizer := range []string{meta.ControllerFinalizer, meta.LegacyResourceFinalizer} {
			if slices.Contains(obj.GetFinalizers(), finalizer) || (finalizer == meta.ControllerFinalizer && retainCleanup) {
				controllerutil.AddFinalizer(fresh, finalizer)
			} else {
				controllerutil.RemoveFinalizer(fresh, finalizer)
			}
		}

		metadata := map[string]any{"resourceVersion": fresh.GetResourceVersion(), "finalizers": fresh.GetFinalizers()}
		if manualRequest != "" && fresh.GetAnnotations()[meta.ReconcileAnnotation] == manualRequest {
			metadata["annotations"] = map[string]any{meta.ReconcileAnnotation: nil}
		}

		payload, err := json.Marshal(map[string]any{"metadata": metadata})
		if err != nil {
			return err
		}

		return c.Patch(ctx, fresh, client.RawPatch(types.MergePatchType, payload))
	})
}
