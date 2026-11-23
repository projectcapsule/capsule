// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// A full gather is a snapshot. Retain cleanup records published by a scoped
// writer after that snapshot, then repeat the authoritative gather/prune before
// declaring the source ready. Otherwise suppressing tenant-wide additions could
// leave a new namespace's resources permanently absent from inventory.
type inventoryCheckpoint struct {
	started bool
	items   meta.ProcessedItems
}

func (c *inventoryCheckpoint) start(items meta.ProcessedItems) {
	c.started = true

	c.items = make(meta.ProcessedItems, len(items))

	for i := range items {
		items[i].DeepCopyInto(&c.items[i])
	}
}

func (c *inventoryCheckpoint) merge(latest, applied meta.ProcessedItems) (meta.ProcessedItems, bool) {
	if c == nil {
		return applied, false
	}

	if !c.started {
		return latest, false
	}

	if (len(latest) == 0 && len(c.items) == 0) || reflect.DeepEqual(latest, c.items) {
		return applied, false
	}

	baseline := make(map[gvk.ResourceID]meta.ObjectReferenceStatus, len(c.items))
	for _, item := range c.items {
		baseline[item.ResourceID] = item
	}

	present := sets.New[gvk.ResourceID]()
	for _, item := range applied {
		present.Insert(item.ResourceID)
	}

	for _, item := range latest {
		if present.Has(item.ResourceID) {
			continue
		}

		if before, exists := baseline[item.ResourceID]; exists && reflect.DeepEqual(before, item) {
			continue
		}

		applied = append(applied, item)
		present.Insert(item.ResourceID)
	}

	return applied, true
}

// Grafts the processed items of a single Namespace scope on the status of a replication
// resource, leaving the items of every other Namespace untouched.
//
// The type parameter keeps the accessors bound to the resource they belong to, whereas
// the patching itself is shared: the status holding the processed items is the very same
// for the namespaced and the global replication resources.
type scopedStatusPatcher[T client.Object] struct {
	client client.Client
	reader client.Reader
	// Builds an empty instance to read the latest state into.
	factory func() T
	// Accesses the status shared by every replication resource.
	status func(T) *capsulev1beta2.TenantResourceCommonStatus
}

// Patch merges the given items in the status of the resource,
// reporting them for the given scope only.
// The in memory instance is kept aligned with what has been written.
func (p scopedStatusPatcher[T]) Patch(
	ctx context.Context,
	instance T,
	scope processor.Scope,
	items meta.ProcessedItems,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := p.factory()
		// The uncached reader is mandatory here: a stale list of processed items would
		// resurrect the ones the full reconciliation just pruned.
		if err := p.reader.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
			return client.IgnoreNotFound(err)
		}

		if latest.GetUID() != instance.GetUID() {
			return apierrors.NewNotFound(capsulev1beta2.GroupVersion.WithResource("replications").GroupResource(), instance.GetName())
		}

		if !latest.GetDeletionTimestamp().IsZero() && !controllerutil.ContainsFinalizer(latest, meta.ControllerFinalizer) {
			return apierrors.NewNotFound(capsulev1beta2.GroupVersion.WithResource("replications").GroupResource(), instance.GetName())
		}

		if !controllerutil.ContainsFinalizer(latest, meta.ControllerFinalizer) {
			original := append([]string(nil), latest.GetFinalizers()...)
			controllerutil.AddFinalizer(latest, meta.ControllerFinalizer)

			if err := patchReplicationMetadata(ctx, p.client, p.reader, latest, original, ""); err != nil {
				return err
			}

			if err := p.reader.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
				return err
			}

			if latest.GetUID() != instance.GetUID() {
				return apierrors.NewNotFound(capsulev1beta2.GroupVersion.WithResource("replications").GroupResource(), instance.GetName())
			}
		}

		status := p.status(latest)
		original := status.DeepCopy()

		status.ProcessedItems.ReplaceScope(scope.Tenant, scope.Namespace, items)
		status.UpdateStats()

		if spec, _ := healthSource(latest); spec.HealthChecks.Enabled() && !sameHealthScanInventory(status.ProcessedItems, original.ProcessedItems) {
			// A scoped apply can outlive the generation that enabled health checks.
			// Retain its cleanup inventory, but only a full apply can pin every
			// target to the current specification and execution identity.
			invalidateHealthApplication(latest, status, time.Now())
		}

		// Only the shared status is compared, since it is the only one being mutated: the
		// resource specific part cannot have drifted.
		if reflect.DeepEqual(*original, *status) {
			return nil
		}

		if err := p.client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		*p.status(instance) = *status

		return nil
	})
}

// Builds the processor options out of the settings shared by every replication resource.
//
// Full and namespace-scoped reconciliation must use the same field owner and fallback settings.
//
//nolint:staticcheck // Stored resource blocks without a policy still use the deprecated settings.
func replicationProcessorOptions(
	obj client.Object,
	spec *capsulev1beta2.TenantResourceCommonSpec,
	owner *metav1.OwnerReference,
) processor.ProcessorOptions {
	return processor.ProcessorOptions{
		TrackHealth:      spec.HealthChecks.Enabled() && obj.GetDeletionTimestamp().IsZero(),
		FieldOwnerPrefix: getFieldOwner(obj.GetName(), obj.GetNamespace()),
		Prune:            *spec.PruningOnDelete,
		Adopt:            *spec.Settings.Adopt,
		Force:            *spec.Settings.Force,
		Owner:            owner,
	}
}

// States whether the given replication resource is out of the Namespace scoped replication
// duties, along with the reason.
// Replicating the same mechanism of reconciling GlobalTenantResource or TenantResource objects.
func skipScopedReplication(
	obj client.Object,
	spec *capsulev1beta2.TenantResourceCommonSpec,
) (bool, string) {
	if !obj.GetDeletionTimestamp().IsZero() {
		return true, "is being deleted"
	}

	if spec.IsCordoned() {
		return true, "is cordoned"
	}

	return false, ""
}
