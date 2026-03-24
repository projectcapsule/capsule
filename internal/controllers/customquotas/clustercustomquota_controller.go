// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

type clusterCustomQuotaClaimController struct {
	client.Client

	log      logr.Logger
	recorder record.EventRecorder

	admissionNotifier chan event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota]
	cache             *cache.QuantityCache[string]
}

func (r *clusterCustomQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalCustomQuota{}).
		WatchesRawSource(
			source.Channel(
				r.admissionNotifier,
				&handler.TypedEnqueueRequestForObject[*capsulev1beta2.GlobalCustomQuota]{},
			),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Watches(&capsulev1beta2.GlobalCustomQuota{}, handler.Funcs{
			CreateFunc: r.OnCreate,
			UpdateFunc: r.OnUpdate,
		}).
		Complete(r)
}

//nolint:dupl
func (r *clusterCustomQuotaClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.GlobalCustomQuota{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return result, err
	}

	return ctrl.Result{}, nil
}

func (r *clusterCustomQuotaClaimController) OnCreate(ctx context.Context, e event.TypedCreateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	//nolint:forcetypeassert
	cq := e.Object.(*capsulev1beta2.GlobalCustomQuota)

	cq.Status = capsulev1beta2.CustomQuotaStatus{}

	namespaces, err := selectors.GetNamespacesMatchingSelectorsStrings(ctx, r.Client, cq.Spec.NamespaceSelectors)
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
		val, err := clt.GetUsageFromUnstructured(item, cq.Spec.Source.Path)
		if err != nil {
			r.log.Error(err, "Error getting usage from unstructured while updating CustomQuota usage")

			continue
		}

		quant, err := resource.ParseQuantity(val)
		if err != nil {
			r.log.Error(err, "Error parsing quantity while updating CustomQuota usage")

			continue
		}

		cq.Status.Usage.Used.Add(quant)

		cq.Status.Claims = append(cq.Status.Claims, meta.NamespacedObjectWithUIDReference{
			Name:      item.GetName(),
			Namespace: meta.RFC1123SubdomainName(item.GetNamespace()),
			UID:       types.UID(item.GetUID()),
		})
		changed = true
	}

	if !changed {
		return
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &capsulev1beta2.GlobalCustomQuota{}

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
	customQuotaOld := e.ObjectOld.(*capsulev1beta2.GlobalCustomQuota)
	//nolint:forcetypeassert
	customQuotaNew := e.ObjectNew.(*capsulev1beta2.GlobalCustomQuota)

	if equality.Semantic.DeepEqual(customQuotaOld.Spec.ScopeSelectors, customQuotaNew.Spec.ScopeSelectors) &&
		equality.Semantic.DeepEqual(customQuotaOld.Spec.Source, customQuotaNew.Spec.Source) &&
		equality.Semantic.DeepEqual(customQuotaOld.Spec.NamespaceSelectors, customQuotaNew.Spec.NamespaceSelectors) {
		return
	}

	customQuotaNew.Status = capsulev1beta2.CustomQuotaStatus{}

	namespaces, err := selectors.GetNamespacesMatchingSelectorsStrings(ctx, r.Client, customQuotaNew.Spec.NamespaceSelectors)
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
		val, err := clt.GetUsageFromUnstructured(item, customQuotaNew.Spec.Source.Path)
		if err != nil {
			r.log.Error(err, "Error getting usage from unstructured while updating CustomQuota usage")

			continue
		}

		quant, err := resource.ParseQuantity(val)
		if err != nil {
			r.log.Error(err, "Error parsing quantity while updating CustomQuota usage")

			continue
		}

		customQuotaNew.Status.Usage.Used.Add(quant)

		customQuotaNew.Status.Claims = append(customQuotaNew.Status.Claims, meta.NamespacedObjectWithUIDReference{
			Name:      item.GetName(),
			Namespace: meta.RFC1123SubdomainName(item.GetNamespace()),
			UID:       types.UID(item.GetUID()),
		})

		changed = true
	}

	if !changed {
		return
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &capsulev1beta2.GlobalCustomQuota{}

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
