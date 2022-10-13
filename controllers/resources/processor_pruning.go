// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

func (r *Processor) HandlePruning(ctx context.Context, current []capsulev1beta2.ObjectReferenceStatus, desired sets.String) (updateStatus bool) {
	log := ctrllog.FromContext(ctx)
	// The status items are the actual replicated resources, these must be collected in order to perform the resulting
	// diff that will be cleaned-up.
	status := sets.NewString()

	for _, item := range current {
		status.Insert(item.String())
	}

	diff := status.Difference(desired)
	// We don't want to trigger a reconciliation of the Status every time,
	// rather, only in case of a difference between the processed and the actual status.
	// This can happen upon the first reconciliation, or a removal, or a change, of a resource.
	updateStatus = diff.Len() > 0 || status.Len() != desired.Len()

	if diff.Len() > 0 {
		log.Info("starting processing pruning", "length", diff.Len())
	}

	// The outer resources must be removed, iterating over these to clean-up
	for item := range diff {
		or := capsulev1beta2.ObjectReferenceStatus{}
		if err := or.ParseFromString(item); err != nil {
			log.Error(err, "unable to parse resource to prune", "resource", item)

			continue
		}

		obj := unstructured.Unstructured{}
		obj.SetNamespace(or.Namespace)
		obj.SetName(or.Name)
		obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(or.APIVersion, or.Kind))

		if err := r.unstructuredClient.Delete(ctx, &obj); err != nil {
			if apierr.IsNotFound(err) {
				// Object may have been already deleted, we can ignore this error
				continue
			}

			log.Error(err, "unable to prune resource", "resource", item)

			continue
		}

		log.Info("resource has been pruned", "resource", item)
	}

	return updateStatus
}
