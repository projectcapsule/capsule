// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type Global struct {
	client    client.Client
	processor Processor
}

func (r *Global) enqueueRequestFromTenant(object client.Object) (reqs []reconcile.Request) {
	tnt := object.(*capsulev1beta2.Tenant) //nolint:forcetypeassert

	resList := capsulev1beta2.GlobalTenantResourceList{}
	if err := r.client.List(context.Background(), &resList); err != nil {
		return nil
	}

	set := sets.NewString()

	for _, res := range resList.Items {
		selector, err := metav1.LabelSelectorAsSelector(&res.Spec.TenantSelector)
		if err != nil {
			continue
		}

		if selector.Matches(labels.Set(tnt.GetLabels())) {
			set.Insert(res.GetName())
		}
	}
	// No need of ordered value here
	for res := range set {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: res,
			},
		})
	}

	return reqs
}

func (r *Global) SetupWithManager(mgr ctrl.Manager) error {
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
	r.processor = Processor{
		client:             r.client,
		unstructuredClient: unstructuredCachingClient,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalTenantResource{}).
		Watches(&source.Kind{Type: &capsulev1beta2.Tenant{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueRequestFromTenant)).
		Complete(r)
}

func (r *Global) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("start processing")

	tntResource := capsulev1beta2.GlobalTenantResource{}
	if err := r.client.Get(ctx, request.NamespacedName, &tntResource); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}
	// Adding the default value for the status
	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, 0)
	}
	// Handling the finalizer section for the given GlobalTenantResource
	enqueueBack, err := r.processor.HandleFinalizer(ctx, &tntResource, *tntResource.Spec.PruningOnDelete, tntResource.Status.ProcessedItems)
	if err != nil || enqueueBack {
		return reconcile.Result{}, err
	}
	// Retrieving the list of the Tenants up to the selector provided by the GlobalTenantResource resource.
	tntSelector, err := metav1.LabelSelectorAsSelector(&tntResource.Spec.TenantSelector)
	if err != nil {
		log.Error(err, "cannot create MatchingLabelsSelector for Global filtering")

		return reconcile.Result{}, err
	}

	tntList := capsulev1beta2.TenantList{}
	if err = r.client.List(ctx, &tntList, &client.MatchingLabelsSelector{Selector: tntSelector}); err != nil {
		log.Error(err, "cannot list Tenants matching the provided selector")

		return reconcile.Result{}, err
	}
	// This is the list of newer Tenants that are matching the provided GlobalTenantResource Selector:
	// upon replication and pruning, this will be updated in the status of the resource.
	tntSet := sets.NewString()

	err = new(multierror.Error)
	// A TenantResource is made of several Resource sections, each one with specific options:
	// the Status can be updated only in case of no errors across all of them to guarantee a valid and coherent status.
	processedItems := sets.NewString()

	for index, resource := range tntResource.Spec.Resources {
		tenantLabel, labelErr := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
		if labelErr != nil {
			log.Error(labelErr, "expected label for selection")

			return reconcile.Result{}, labelErr
		}

		for _, tnt := range tntList.Items {
			tntSet.Insert(tnt.GetName())

			items, sectionErr := r.processor.HandleSection(ctx, tnt, true, tenantLabel, index, resource)
			if sectionErr != nil {
				// Upon a process error storing the last error occurred and continuing to iterate,
				// avoid to block the whole processing.
				err = multierror.Append(err, sectionErr)
			} else {
				processedItems.Insert(items...)
			}
		}
	}

	if err.(*multierror.Error).ErrorOrNil() != nil { //nolint:errorlint,forcetypeassert
		log.Error(err, "unable to replicate the requested resources")

		return reconcile.Result{}, err
	}

	shouldUpdateStatus := !sets.NewString(tntResource.Status.SelectedTenants...).Equal(tntSet)

	if r.processor.HandlePruning(ctx, tntResource.Status.ProcessedItems, processedItems) {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))

		for _, item := range processedItems.List() {
			if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
			}
		}

		shouldUpdateStatus = true
	}

	if shouldUpdateStatus {
		tntResource.Status.SelectedTenants = tntSet.List()

		if updateErr := r.client.Status().Update(ctx, &tntResource); updateErr != nil {
			log.Error(updateErr, "unable to update TenantResource status")
		}
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}
