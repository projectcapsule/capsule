// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"reflect"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type globalResourceController struct {
	client        client.Client
	log           logr.Logger
	processor     Processor
	configuration configuration.Configuration
	metrics       *metrics.GlobalTenantResourceRecorder
}

func (r *globalResourceController) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.processor = Processor{
		client: mgr.GetClient(),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalTenantResource{}).
		Watches(&capsulev1beta2.Tenant{}, handler.EnqueueRequestsFromMapFunc(r.enqueueRequestFromTenant)).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
				var list capsulev1beta2.GlobalTenantResourceList
				if err := r.client.List(ctx, &list); err != nil {
					r.log.Error(err, "unable to list GlobalTenantResources")

					return nil
				}

				var requests []reconcile.Request
				for _, s := range list.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      s.Name,
							Namespace: s.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldObj, okOld := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
					newObj, okNew := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)
					if !okOld || !okNew {
						return false
					}

					return !reflect.DeepEqual(oldObj.Spec.ServiceAccountClient, newObj.Spec.ServiceAccountClient)
				},
				DeleteFunc: func(event.DeleteEvent) bool {
					return false
				},
			}),
		).
		Complete(r)
}

func (r *globalResourceController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var err error

	log := ctrllog.FromContext(ctx)

	log.V(5).Info("start processing")
	// Retrieving the GlobalTenantResource
	tntResource := &capsulev1beta2.GlobalTenantResource{}
	if err = r.client.Get(ctx, request.NamespacedName, tntResource); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteMetrics(request.Name)

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
				err = gherrors.Wrap(e, "failed to patch GlobalTenantResource")
			}
		}
	}()

	c, err := r.loadClient(ctx, log, tntResource)
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to load serviceaccount client")
	}

	if c == nil {
		log.V(5).Info("received empty client for serviceaccount")

		return reconcile.Result{}, nil
	}

	// Handle deleted GlobalTenantResource
	if !tntResource.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, c, tntResource)
	}

	// Handle non-deleted GlobalTenantResource
	return r.reconcileNormal(ctx, c, tntResource)
}

func (r *globalResourceController) enqueueRequestFromTenant(ctx context.Context, object client.Object) (reqs []reconcile.Request) {
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

func (r *globalResourceController) reconcileNormal(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.GlobalTenantResource,
) (reconcile.Result, error) {
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

	// Use Controller Client.
	tntList := capsulev1beta2.TenantList{}
	if err = r.client.List(ctx, &tntList, &client.MatchingLabelsSelector{Selector: tntSelector}); err != nil {
		log.Error(err, "cannot list Tenants matching the provided selector")

		return reconcile.Result{}, err
	}
	// This is the list of newer Tenants that are matching the provided GlobalTenantResource Selector:
	// upon replication and pruning, this will be updated in the status of the resource.
	tntSet := sets.NewString()

	// A TenantResource is made of several Resource sections, each one with specific options:
	// the Status can be updated only in case of no errors across all of them to guarantee a valid and coherent status.
	processedItems := sets.NewString()

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

	var itemErrors error

	for index, resource := range tntResource.Spec.Resources {
		tenantLabel, labelErr := capsulev1beta2.GetTypeLabel(&capsulev1beta2.Tenant{})
		if labelErr != nil {
			log.Error(labelErr, "expected label for selection")

			return reconcile.Result{}, labelErr
		}

		for _, tnt := range tntList.Items {
			tntSet.Insert(tnt.GetName())

			items, sectionErr := r.processor.HandleSectionPreflight(ctx, c, tnt, true, tenantLabel, index, resource, tntResource.Spec.Scope)
			if sectionErr != nil {
				// Upon a process error storing the last error occurred and continuing to iterate,
				// avoid to block the whole processing.
				itemErrors = errors.Join(itemErrors, sectionErr)
			}

			log.Info("replicate items", "amount", len(items))

			processedItems.Insert(items...)
		}
	}

	if err != nil {
		log.Error(err, "unable to replicate the requested resources")

		return reconcile.Result{}, err
	}

	failed, err := r.processor.HandlePruning(ctx, c, tntResource.Status.ProcessedItems.AsSet(), sets.Set[string](processedItems))
	if err != nil {
		return reconcile.Result{}, gherrors.Wrap(err, "failed to prune resources")
	}
	if len(failed) > 0 {
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

func (r *globalResourceController) reconcileDelete(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.GlobalTenantResource,
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

		controllerutil.RemoveFinalizer(tntResource, finalizer)
	}

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *globalResourceController) loadClient(
	ctx context.Context,
	log logr.Logger,
	tntResource *capsulev1beta2.GlobalTenantResource,
) (client.Client, error) {
	// Add ServiceAccount if required, Retriggers reconcile
	// This is done in the background, Everything else should be handeled at admission
	if changed := SetGlobalTenantResourceServiceAccount(r.configuration, tntResource); changed {
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
