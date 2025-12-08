// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type globalResourceController struct {
	client        client.Client
	log           logr.Logger
	processor     Processor
	configuration configuration.Configuration
	metrics       *metrics.GlobalTenantResourceRecorder
}

func (r *globalResourceController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.client = mgr.GetClient()
	r.processor = Processor{
		client:                       mgr.GetClient(),
		factory:                      serializer.NewCodecFactory(r.client.Scheme()),
		configuration:                r.configuration,
		allowCrossNamespaceSelection: false,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalTenantResource{}).
		Watches(&capsulev1beta2.Tenant{}, handler.EnqueueRequestsFromMapFunc(r.enqueueRequestFromTenant)).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *globalResourceController) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
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
		if uerr := r.updateStatus(ctx, tntResource, err); uerr != nil {
			err = fmt.Errorf("cannot update globaltenantresource status: %w", uerr)

			return
		}

		r.metrics.RecordConditions(tntResource)

		if e := patchHelper.Patch(ctx, tntResource); e != nil {
			if err == nil {
				err = gherrors.Wrap(e, "failed to patch GlobalTenantResource")
			}
		}
	}()

	if *tntResource.Spec.Cordoned {
		log.V(5).Info("tenant resource is cordoned")
	}

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
) (res reconcile.Result, err error) {
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
	//processedItems := sets.NewString()

	// Always post the processed items, as they allow users to track errors
	//defer func() {
	//	tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))
	//
	//	for _, item := range processedItems.List() {
	//		log.Info("PROCESSED", "ITEM", item)
	//
	//		or := capsulev1beta2.ObjectReferenceStatus{}
	//		if parseErr := or.ParseFromString(item); parseErr == nil {
	//			tntResource.Status.ProcessedItems.UpdateItem(or)
	//		} else {
	//			err = errors.Join(err, fmt.Errorf("processed item %q parse failed: %w", item, parseErr))
	//		}
	//
	//		log.Info("PARSED", "OR", or)
	//	}
	//
	//	log.Info("STATUS", "STATUS", tntResource.Status)
	//
	//}()

	//status := capsulev1beta2.ProcessedItems{}
	acc := Accumulator{}

	// Gather Resources
	for index, resource := range tntResource.Spec.Resources {
		for _, tnt := range tntList.Items {
			var resourceError error

			tplContext, _ := resource.Context.GatherContext(ctx, c, nil, "")
			tplContext["Tenant"] = tnt

			switch tntResource.Spec.Scope {
			case api.ResourceScopeTenant:
				//tplContext, _ = spec.Context.GatherContext(ctx, c, nil, "")
				//tplContext["Tenant"] = tnt

				//owner := fieldOwner + "/" + tnt.Name + "/"

				resourceError = r.processor.handleResources(
					ctx,
					c,
					tnt,
					strconv.Itoa(index),
					resource,
					nil,
					tplContext,
					acc,
				)
			default:
				resourceError = r.processor.foreachTenantNamespace(ctx, c, tnt, resource, strconv.Itoa(index), tplContext, acc)

			}

			// Only start pruning when the resource item itself did not throw an error
			if resourceError != nil {
				return reconcile.Result{}, resourceError
			}
		}
	}

	// Prune first, to work on a consistent Status
	for _, p := range tntResource.Status.ProcessedItems {
		if _, exists := acc[p.ResourceIDWithOptions]; !exists {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(p.GetGVK())
			obj.SetNamespace(p.GetNamespace())
			obj.SetName(p.GetName())

			if *p.Prune {
				err := r.processor.Prune(ctx, c, obj, getFieldOwner(tntResource.GetName(), "", p.ResourceID))
				if err != nil {
					p.Status = metav1.ConditionFalse
					p.Message = err.Error()
					tntResource.Status.ProcessedItems.UpdateItem(p)

					continue
				}
			}

			tntResource.Status.ProcessedItems.RemoveItem(p)
		}
	}
	//
	log.Info("accumulation", "items", len(acc))

	// Apply
	for id, obj := range acc {
		or := capsulev1beta2.ObjectReferenceStatus{
			ResourceIDWithOptions: id,
			ObjectReferenceStatusCondition: capsulev1beta2.ObjectReferenceStatusCondition{
				Type: meta.ReadyCondition,
			},
		}

		//id.Index

		err := r.processor.Apply(
			ctx,
			c,
			obj,
			getFieldOwner(tntResource.GetName(), "", id.ResourceID),
			*id.Force,
			*id.Adopt,
		)
		if err != nil {
			or.Status = metav1.ConditionTrue
			or.Message = err.Error()
		} else {
			or.Status = metav1.ConditionTrue
		}

		tntResource.Status.ProcessedItems.UpdateItem(or)
	}

	// Prune Resources
	//failed, err := r.processor.HandlePruning(ctx, c, tntResource.Status.ProcessedItems.AsSet(), sets.Set[string](processedItems))
	//if err != nil {
	//	return reconcile.Result{}, gherrors.Wrap(err, "failed to prune resources")
	//}
	//if len(failed) > 0 {
	//	tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(processedItems))
	//
	//	for _, item := range processedItems.List() {
	//		if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
	//			tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
	//		}
	//	}
	//}

	tntResource.Status.SelectedTenants = tntSet.List()

	log.Info("processing completed")

	return reconcile.Result{Requeue: true, RequeueAfter: tntResource.Spec.ResyncPeriod.Duration}, nil
}

func (r *globalResourceController) reconcileDelete(
	ctx context.Context,
	c client.Client,
	tntResource *capsulev1beta2.GlobalTenantResource,
) (reconcile.Result, error) {
	//_ := ctrllog.FromContext(ctx)

	//if *tntResource.Spec.PruningOnDelete {
	//	failedItems, err := r.processor.HandlePruning(ctx, c, tntResource.Status.ProcessedItems.AsSet(), nil)
	//	if len(failedItems) > 0 {
	//		tntResource.Status.ProcessedItems = make([]capsulev1beta2.ObjectReferenceStatus, 0, len(failedItems))
	//
	//		for _, item := range failedItems {
	//			if or := (capsulev1beta2.ObjectReferenceStatus{}); or.ParseFromString(item) == nil {
	//				tntResource.Status.ProcessedItems = append(tntResource.Status.ProcessedItems, or)
	//			}
	//		}
	//	}
	//
	//	if len(failedItems) > 0 || err != nil {
	//		return reconcile.Result{}, gherrors.Wrap(err, "failed to prune resources on delete")
	//	}
	//
	//	controllerutil.RemoveFinalizer(tntResource, finalizer)
	//}
	//
	//log.Info("processing completed")

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

		saClient, err = users.ImpersonatedKubernetesClientForServiceAccount(
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

func (r *globalResourceController) updateStatus(ctx context.Context, instance *capsulev1beta2.GlobalTenantResource, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalTenantResource{}
		if err = r.client.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			return err
		}

		latest.Status = instance.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		// Set Cordoned Condition
		cordonedCondition := meta.NewCordonedCondition(instance)

		if *instance.Spec.Cordoned {
			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		}

		latest.Status.Conditions.UpdateConditionByType(cordonedCondition)

		return r.client.Status().Update(ctx, latest)
	})
}
