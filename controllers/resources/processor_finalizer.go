// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

func (r *Processor) HandleFinalizer(ctx context.Context, obj client.Object, shouldPrune bool, items []capsulev1beta2.ObjectReferenceStatus) (enqueueBack bool, err error) {
	log := ctrllog.FromContext(ctx)
	// If the object has been marked for deletion,
	// we have to clean up the created resources before removing the finalizer.
	if obj.GetDeletionTimestamp() != nil {
		log.Info("pruning prior finalizer removal")

		if shouldPrune {
			_ = r.HandlePruning(ctx, items, nil)
		}

		obj.SetFinalizers(nil)

		if err = r.client.Update(ctx, obj); err != nil {
			log.Error(err, "cannot remove finalizer")

			return true, err
		}

		return true, nil
	}
	// When the pruning for the given resource is enabled, a finalizer is required when the TenantResource is marked
	// for deletion: this allows to perform a clean-up of all the underlying resources.
	if shouldPrune && !sets.NewString(obj.GetFinalizers()...).Has(finalizer) {
		obj.SetFinalizers(append(obj.GetFinalizers(), finalizer))

		if err = r.client.Update(ctx, obj); err != nil {
			log.Error(err, "cannot add finalizer")

			return true, err
		}

		log.Info("added finalizer, enqueuing back for processing")

		return true, nil
	}

	return false, nil
}
