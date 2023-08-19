// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

type Global struct {
	client    client.Client
	processor Processor
}

func (r *Global) enqueueRequestFromTenant(ctx context.Context, object client.Object) (reqs []reconcile.Request) {
	tnt := object.(*capsulev1beta2.Tenant) //nolint:forcetypeassert

	resList := capsulev1beta2.GlobalTenantResourceList{}
	if err := r.client.List(ctx, &resList); err != nil {
		return nil
	}

	set := sets.NewString()

	for _, res := range resList.Items {
		tntSelector := res.Spec.TenantSelector

		selector, err := metav1.LabelSelectorAsSelector(&tntSelector)
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
	r.client = mgr.GetClient()
	r.processor = Processor{
		client: mgr.GetClient(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalTenantResource{}).
		Watches(&capsulev1beta2.Tenant{}, handler.EnqueueRequestsFromMapFunc(r.enqueueRequestFromTenant)).
		Complete(r)
}

//nolint:dupl
func (r *Global) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	log.Info("start processing")
	// Retrieving the GlobalTenantResource
	tntResource := &capsulev1beta2.GlobalTenantResource{}
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
				err = errors.Wrap(e, "failed to patch GlobalTenantResource")
			}
		}
	}()

	// Handle deleted GlobalTenantResource
	if !tntResource.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, tntResource)
	}

	// Handle non-deleted GlobalTenantResource
	return r.reconcileNormal(ctx, tntResource)
}

func (r *Global) reconcileNormal(ctx context.Context, tntResource *capsulev1beta2.GlobalTenantResource) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	if *tntResource.Spec.PruningOnDelete {
		controllerutil.AddFinalizer(tntResource, finalizer)
	}

	if tntResource.Status.ProcessedItems == nil {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0)
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

	if r.processor.HandlePruning(ctx, tntResource.Status.ProcessedItems.AsSet(), sets.Set[string](processedItems)) {
		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))

		for _, item := range processedItems.List() {
			if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
			}
		}
	}

	tntResource.Status.SelectedTenants = tntSet.List()

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *Global) reconcileDelete(ctx context.Context, tntResource *capsulev1beta2.GlobalTenantResource) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)

	if *tntResource.Spec.PruningOnDelete {
		r.processor.HandlePruning(ctx, tntResource.Status.ProcessedItems.AsSet(), nil)

		controllerutil.RemoveFinalizer(tntResource, finalizer)
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}
