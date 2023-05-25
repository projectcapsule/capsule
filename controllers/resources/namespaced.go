// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type Namespaced struct {
	client    client.Client
	processor Processor
}

func (r *Namespaced) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.processor = Processor{
		client: mgr.GetClient(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.TenantResource{}).
		Complete(r)
}

//nolint:dupl
func (r *Namespaced) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("start processing")
	// Retrieving the TenantResource
	tntResource := &capsulev1beta2.TenantResource{}
	if err := r.client.Get(ctx, request.NamespacedName, tntResource); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	patchHelper, err := patch.NewHelper(tntResource, r.client)
	if err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to init patch helper")
	}

	defer func() {
		if e := patchHelper.Patch(ctx, tntResource); e != nil {
			if err == nil {
				err = errors.Wrap(e, "failed to patch TenantResource")
			}
		}
	}()

	// Handle deleted TenantResource
	if !tntResource.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, tntResource)
	}

	// Handle non-deleted TenantResource
	return r.reconcileNormal(ctx, tntResource)
}

func (r *Namespaced) reconcileNormal(ctx context.Context, tntResource *capsulev1beta2.TenantResource) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	if *tntResource.Spec.PruningOnDelete {
		controllerutil.AddFinalizer(tntResource, finalizer)
	}

	// Adding the default value for the status
	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0)
	}

	// Retrieving the parent of the Tenant Resource:
	// can be owned, or being deployed in one of its Namespace.
	tl := &capsulev1beta2.TenantList{}
	if err := r.client.List(ctx, tl, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", tntResource.GetNamespace())}); err != nil {
		log.Error(err, "unable to detect the Tenant for the given TenantResource")

		return reconcile.Result{}, err
	}

	if len(tl.Items) == 0 {
		log.Info("skipping sync, the current Namespace is not belonging to any Global")

		return reconcile.Result{}, nil
	}

	err := new(multierror.Error)
	// A TenantResource is made of several Resource sections, each one with specific options:
	// the Status can be updated only in case of no errors across all of them to guarantee a valid and coherent status.
	processedItems := sets.NewString()

	tenantLabel, labelErr := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
	if labelErr != nil {
		log.Error(labelErr, "expected label for selection")

		return reconcile.Result{}, labelErr
	}

	for index, resource := range tntResource.Spec.Resources {
		items, sectionErr := r.processor.HandleSection(ctx, tl.Items[0], false, tenantLabel, index, resource)
		if sectionErr != nil {
			// Upon a process error storing the last error occurred and continuing to iterate,
			// avoid to block the whole processing.
			err = multierror.Append(err, sectionErr)
		} else {
			processedItems.Insert(items...)
		}
	}

	if err.ErrorOrNil() != nil {
		log.Error(err, "unable to replicate the requested resources")

		return reconcile.Result{}, err
	}

	if r.processor.HandlePruning(ctx, tntResource.Status.ProcessedItems.AsSet(), sets.Set[string](processedItems)) {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))

		for _, item := range processedItems.List() {
			if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
			}
		}
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *Namespaced) reconcileDelete(ctx context.Context, tntResource *capsulev1beta2.TenantResource) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	if *tntResource.Spec.PruningOnDelete {
		r.processor.HandlePruning(ctx, tntResource.Status.ProcessedItems.AsSet(), nil)
	}

	controllerutil.RemoveFinalizer(tntResource, finalizer)

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}
