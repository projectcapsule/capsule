// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/hashicorp/go-multierror"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type Namespaced struct {
	client    client.Client
	finalizer Processor
}

func (r *Namespaced) SetupWithManager(mgr ctrl.Manager) error {
	unstructuredCachingClient, err := client.NewDelegatingClient(
		client.NewDelegatingClientInput{
			Client:            mgr.GetClient(),
			CacheReader:       mgr.GetCache(),
			CacheUnstructured: true,
		},
	)
	if err != nil {
		return err
	}

	r.client = mgr.GetClient()
	r.finalizer = Processor{
		client:             r.client,
		unstructuredClient: unstructuredCachingClient,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.TenantResource{}).
		Complete(r)
}

func (r *Namespaced) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("start processing")
	// Retrieving the TenantResource
	tntResource := capsulev1beta2.TenantResource{}
	if err := r.client.Get(ctx, request.NamespacedName, &tntResource); err != nil {
		if apierr.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		log.Error(err, "cannot retrieve capsulev1beta2.TenantResource")

		return reconcile.Result{}, err
	}
	// Adding the default value for the status
	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, 0)
	}
	// Handling the finalizer section for the given TenantResource
	enqueueBack, err := r.finalizer.HandleFinalizer(ctx, &tntResource, *tntResource.Spec.PruningOnDelete, tntResource.Status.ProcessedItems)
	if err != nil || enqueueBack {
		return reconcile.Result{}, err
	}
	// Retrieving the parent of the Global Resource:
	// can be owned, or being deployed in one of its Namespace.
	tl := &capsulev1beta2.TenantList{}
	if err = r.client.List(ctx, tl, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", tntResource.GetNamespace())}); err != nil {
		log.Error(err, "unable to detect the Global for the given TenantResource")

		return reconcile.Result{}, err
	}

	if len(tl.Items) == 0 {
		log.Info("skipping sync, the current Namespace is not belonging to any Global")

		return reconcile.Result{}, nil
	}

	err = new(multierror.Error)
	// A TenantResource is made of several Resource sections, each one with specific options:
	// the Status can be updated only in case of no errors across all of them to guarantee a valid and coherent status.
	processedItems := sets.NewString()

	tenantLabel, labelErr := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
	if labelErr != nil {
		log.Error(labelErr, "expected label for selection")

		return reconcile.Result{}, labelErr
	}

	for index, resource := range tntResource.Spec.Resources {
		items, sectionErr := r.finalizer.HandleSection(ctx, tl.Items[0], false, tenantLabel, index, resource)
		if sectionErr != nil {
			// Upon a process error storing the last error occurred and continuing to iterate,
			// avoid to block the whole processing.
			err = multierror.Append(err, sectionErr)
		} else {
			processedItems.Insert(items...)
		}
	}

	if err.(*multierror.Error).ErrorOrNil() != nil { //nolint:errorlint,forcetypeassert
		log.Error(err, "unable to replicate the requested resources")

		return reconcile.Result{}, err
	}

	if r.finalizer.HandlePruning(ctx, tntResource.Status.ProcessedItems, processedItems) {
		statusErr := retry.RetryOnConflict(retry.DefaultRetry, func() (err error) {
			if err = r.client.Get(ctx, request.NamespacedName, &tntResource); err != nil {
				return err
			}

			tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))

			for _, item := range processedItems.List() {
				if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
					tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
				}
			}

			return r.client.Status().Update(ctx, &tntResource)
		})
		if statusErr != nil {
			log.Error(statusErr, "unable to update TenantResource status")
		}
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}
