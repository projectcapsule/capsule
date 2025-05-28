// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type namespacedResourceController struct {
	client        client.Client
	log           logr.Logger
	processor     Processor
	configuration configuration.Configuration
	metrics       *metrics.TenantResourceRecorder
}

func (r *namespacedResourceController) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.processor = Processor{
		client: mgr.GetClient(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.TenantResource{}).
		Complete(r)
}

func (r *namespacedResourceController) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	log := ctrllog.FromContext(ctx)

	log.V(5).Info("start processing")
	// Retrieving the TenantResource
	tntResource := &capsulev1beta2.TenantResource{}
	if err := r.client.Get(ctx, request.NamespacedName, tntResource); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteMetric(request.Name)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	patchHelper, err := patch.NewHelper(tntResource, r.client)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to init patch helper")
	}

	defer func() {
		r.metrics.RecordCondition(tntResource)

		tntResource.SetCondition()
		if e := patchHelper.Patch(ctx, tntResource); e != nil {
			if err == nil {
				err = gherrors.Wrap(e, "failed to patch TenantResource")
			}
		}
	}()

	c, err := r.loadClient(ctx, log, tntResource)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to load serviceaccount client")
	}
	if c == nil {
		log.V(3).Info("received empty client for serviceaccount")
		return reconcile.Result{}, nil
	}

	// Handle deleted TenantResource
	if !tntResource.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, c, tntResource)
	}

	// Handle non-deleted TenantResource
	return r.reconcileNormal(ctx, c, tntResource)
}

func (r *namespacedResourceController) reconcileNormal(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.TenantResource,
) (reconcile.Result, error) {
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

	// A TenantResource is made of several Resource sections, each one with specific options:
	// the Status can be updated only in case of no errors across all of them to guarantee a valid and coherent status.
	processedItems := sets.NewString()

	tenantLabel, labelErr := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
	if labelErr != nil {
		log.Error(labelErr, "expected label for selection")

		return reconcile.Result{}, labelErr
	}

	// Always post the processed items, as they allow users to track errors
	defer func() {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))

		for _, item := range processedItems.List() {
			or := capsulev1beta2.ObjectReferenceStatus{}
			if err := or.ParseFromString(item); err == nil {
				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
			} else {
				log.Error(err, "failed to parse processed item", "item", item)
			}
		}
	}()

	// new empty error
	var err error

	for index, resource := range tntResource.Spec.Resources {
		items, sectionErr := r.processor.HandleSection(ctx, c, tl.Items[0], false, tenantLabel, index, resource)
		if sectionErr != nil {
			// Upon a process error storing the last error occurred and continuing to iterate,
			// avoid to block the whole processing.
			err = errors.Join(err, sectionErr)
		}

		processedItems.Insert(items...)
	}

	if err != nil {
		log.Error(err, "unable to replicate the requested resources")

		return reconcile.Result{}, err
	}

	failedItems, err := r.processor.HandlePruning(
		ctx,
		c,
		tntResource.Status.ProcessedItems.AsSet(),
		sets.Set[string](processedItems))
	if len(failedItems) > 0 {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(failedItems))

		for _, item := range failedItems {
			if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
			}
		}
	}

	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to prune resources")
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *namespacedResourceController) reconcileDelete(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.TenantResource,
) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	if *tntResource.Spec.PruningOnDelete {
		failedItems, err := r.processor.HandlePruning(ctx, c, tntResource.Status.ProcessedItems.AsSet(), nil)
		if len(failedItems) > 0 {
			tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(failedItems))

			for _, item := range failedItems {
				if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
					tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
				}
			}
		}

		if len(failedItems) > 0 || err != nil {
			return reconcile.Result{}, gherrors.Wrap(err, "failed to prune resources on delete")
		}

	}

	controllerutil.RemoveFinalizer(tntResource, finalizer)

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *namespacedResourceController) loadClient(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.TenantResource,
) (client.Client, error) {
	// Add ServiceAccount if required, Retriggers reconcile
	// This is done in the background, Everything else should be handeled at admission
	if changed := SetTenantResourceServiceAccount(r.configuration, tntResource); changed {
		log.V(5).Info("adding default serviceAccount '%s'", tntResource.Spec.ServiceAccount.GetFullName())

		return nil, nil
	}

	// Load impersonation client
	saClient := r.client
	if tntResource.Spec.ServiceAccount != nil {
		re, err := r.configuration.ServiceAccountClient(ctx)
		if err != nil {
			log.Error(err, "failed to load impersonated rest client")

			return nil, err
		}

		//utils.NamespacedServiceAccountName()
		//
		saClient, err = utils.ImpersonatedKubernetesClientForServiceAccount(
			re,
			r.client.Scheme(),
			tntResource.Spec.ServiceAccount,
		)
		if err != nil {
			log.Error(err, "failed to create impersonated client")

			return nil, err
		}
	}

	return saClient, nil
}
