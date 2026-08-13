// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

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

		status := p.status(latest)
		original := status.DeepCopy()

		status.ProcessedItems.ReplaceScope(scope.Tenant, scope.Namespace, items)
		status.UpdateStats()

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
// The field owner must be derived exactly as the full reconciliation does, otherwise the
// two would fight over the server-side apply ownership of the very same objects.
func scopedProcessorOptions(
	obj client.Object,
	spec *capsulev1beta2.TenantResourceCommonSpec,
	owner *metav1.OwnerReference,
) processor.ProcessorOptions {
	return processor.ProcessorOptions{
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
