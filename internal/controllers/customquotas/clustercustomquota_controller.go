// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
)

type clusterCustomQuotaClaimController struct {
	client.Client

	log      logr.Logger
	recorder record.EventRecorder
}

func (r *clusterCustomQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ClusterCustomQuota{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Watches(&capsulev1beta2.ClusterCustomQuota{}, handler.Funcs{
			CreateFunc: r.OnCreate,
			UpdateFunc: r.OnUpdate,
		}).
		Complete(r)
}

//nolint:dupl
func (r *clusterCustomQuotaClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.ClusterCustomQuota{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return result, err
	}

	// Ensuring the CustomQuota Status
	if err = r.reconcile(ctx, log, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *clusterCustomQuotaClaimController) OnCreate(ctx context.Context, e event.TypedCreateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	//nolint:forcetypeassert
	cq := e.Object.(*capsulev1beta2.ClusterCustomQuota)

	cq.Status = capsulev1beta2.CustomQuotaStatus{}

	namespaces, err := GetNamespacesMatchingSelectors(ctx, cq.Spec.Selectors, r.Client)
	if err != nil {
		r.log.Error(err, "Error getting namespaces while updating CustomQuota usage")

		return
	}

	items, err := getResources(ctx, &cq.Spec.Source, r.Client, cq.Spec.ScopeSelectors, namespaces...)
	if err != nil {
		r.log.Error(err, "Error getting resources while updating CustomQuota usage")

		return
	}

	changed := false

	for _, item := range items {
		val, err := GetUsageFromUnstructured(item, cq.Spec.Source.Path)
		if err != nil {
			r.log.Error(err, "Error getting usage from unstructured while updating CustomQuota usage")

			continue
		}

		quant, err := resource.ParseQuantity(val)
		if err != nil {
			r.log.Error(err, "Error parsing quantity while updating CustomQuota usage")

			continue
		}

		cq.Status.Used.Add(quant)

		claim := fmt.Sprintf("%s.%s", item.GetNamespace(), item.GetName())
		cq.Status.Claims = append(cq.Status.Claims, claim)
		changed = true
	}

	if !changed {
		return
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &capsulev1beta2.ClusterCustomQuota{}

		nn := client.ObjectKey{Name: cq.Name}

		if getErr := r.Get(ctx, nn, latest); getErr != nil {
			return getErr
		}

		latest.Status = cq.Status

		return r.Client.Status().Update(ctx, latest)
	})
	if retryErr != nil {
		r.log.Error(retryErr, "Error updating ClusterCustomQuota status on creation")
	}
}

func (r *clusterCustomQuotaClaimController) OnUpdate(ctx context.Context, e event.TypedUpdateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	//nolint:forcetypeassert
	customQuotaOld := e.ObjectOld.(*capsulev1beta2.ClusterCustomQuota)
	//nolint:forcetypeassert
	customQuotaNew := e.ObjectNew.(*capsulev1beta2.ClusterCustomQuota)

	if equality.Semantic.DeepEqual(customQuotaOld.Spec.ScopeSelectors, customQuotaNew.Spec.ScopeSelectors) &&
		equality.Semantic.DeepEqual(customQuotaOld.Spec.Source, customQuotaNew.Spec.Source) &&
		equality.Semantic.DeepEqual(customQuotaOld.Spec.Selectors, customQuotaNew.Spec.Selectors) {
		return
	}

	customQuotaNew.Status = capsulev1beta2.CustomQuotaStatus{}

	namespaces, err := GetNamespacesMatchingSelectors(ctx, customQuotaNew.Spec.Selectors, r.Client)
	if err != nil {
		r.log.Error(err, "Error getting namespaces while updating CustomQuota usage")

		return
	}

	items, err := getResources(ctx, &customQuotaNew.Spec.Source, r.Client, customQuotaNew.Spec.ScopeSelectors, namespaces...)
	if err != nil {
		r.log.Error(err, "Error getting resources while updating CustomQuota usage")

		return
	}

	changed := false

	for _, item := range items {
		val, err := GetUsageFromUnstructured(item, customQuotaNew.Spec.Source.Path)
		if err != nil {
			r.log.Error(err, "Error getting usage from unstructured while updating CustomQuota usage")

			continue
		}

		quant, err := resource.ParseQuantity(val)
		if err != nil {
			r.log.Error(err, "Error parsing quantity while updating CustomQuota usage")

			continue
		}

		customQuotaNew.Status.Used.Add(quant)

		claim := fmt.Sprintf("%s.%s", item.GetNamespace(), item.GetName())
		customQuotaNew.Status.Claims = append(customQuotaNew.Status.Claims, claim)
		changed = true
	}

	if !changed {
		return
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &capsulev1beta2.ClusterCustomQuota{}

		nn := client.ObjectKey{Name: customQuotaNew.Name}

		if getErr := r.Get(ctx, nn, latest); getErr != nil {
			return getErr
		}

		latest.Status = customQuotaNew.Status

		return r.Client.Status().Update(ctx, latest)
	})
	if retryErr != nil {
		r.log.Error(retryErr, "Error updating ClusterCustomQuota status on update")
	}
}

// This Controller is responsible for keeping the ClusterCustomQuota Status in sync with the actual usage.
// Everything else will be handled by the CustomQuota Validating Webhook.
func (r *clusterCustomQuotaClaimController) reconcile(
	ctx context.Context,
	log logr.Logger,
	customQuota *capsulev1beta2.ClusterCustomQuota,
) (err error) {
	customQuota.Status.Available = customQuota.Spec.Limit.DeepCopy()
	customQuota.Status.Available.Sub(customQuota.Status.Used)

	return r.Client.Status().Update(ctx, customQuota)
}
