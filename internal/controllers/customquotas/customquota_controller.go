// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
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
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
)

type customQuotaClaimController struct {
	client.Client

	metrics  *metrics.CustomQuotaRecorder
	log      logr.Logger
	recorder record.EventRecorder
	mapper   k8smeta.RESTMapper

	admissionNotifier chan event.TypedGenericEvent[*capsulev1beta2.CustomQuota]
	cache             *cache.QuantityCache[string]
}

func (r *customQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.mapper = mgr.GetRESTMapper()

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.CustomQuota{}).
		WatchesRawSource(
			source.Channel(
				r.admissionNotifier,
				&handler.TypedEnqueueRequestForObject[*capsulev1beta2.CustomQuota]{},
			),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

//nolint:dupl
func (r *customQuotaClaimController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.CustomQuota{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteAllMetricsForCustomQuota(request.Name, request.Namespace)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return result, err
	}

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	defer func() {
		if uerr := r.updateStatus(ctx, instance, err); uerr != nil {
			err = fmt.Errorf("cannot update status: %w", uerr)

			return
		}

		r.emitMetrics(instance)

		if e := patchHelper.Patch(ctx, instance); e != nil {
			err = fmt.Errorf("cannot patch: %w", e)

			return
		}

		err = nil
	}()

	err = r.reconcile(ctx, instance)

	if !r.hasConsistentStateWithCache(instance) {
		log.Info("recomputed usage is not yet consistent with admission cache, requeueing",
			"customQuota", instance.Name,
			"used", instance.Status.Usage.Used.String(),
		)

		return ctrl.Result{RequeueAfter: 500 * time.Millisecond}, nil
	}

	r.cache.Delete(MakeCustomQuotaCacheKey(*instance))

	return ctrl.Result{}, err
}

func (r *customQuotaClaimController) reconcile(
	ctx context.Context,
	instance *capsulev1beta2.CustomQuota,
) error {
	// Rebuilding
	instance.Status.Target = capsulev1beta2.CustomQuotaSpecSource{}
	instance.Status.Usage = capsulev1beta2.CustomQuotaStatusUsage{}
	instance.Status.Claims = nil

	kind := schema.GroupVersionKind{
		Group:   instance.Spec.Source.GroupVersionKind.Group,
		Version: instance.Spec.Source.GroupVersionKind.Version,
		Kind:    instance.Spec.Source.GroupVersionKind.Kind,
	}

	_, err := r.Client.Scheme().New(kind)
	if err != nil {
		return fmt.Errorf("GVK %s is not registered in scheme: %w", kind.String(), err)
	}

	mapping, err := r.mapper.RESTMapping(kind.GroupKind(), kind.Version)
	if err != nil {
		return fmt.Errorf("failed to resolve REST mapping for %s: %w", kind.String(), err)
	}

	if mapping.Scope.Name() != k8smeta.RESTScopeNameNamespace {
		return fmt.Errorf("GVK %s is not namespaced", kind.String())
	}

	instance.Status.Target = instance.Spec.Source
	instance.Status.Usage = capsulev1beta2.CustomQuotaStatusUsage{}

	items, err := getResources(ctx, &instance.Status.Target, r.Client, instance.Spec.ScopeSelectors, instance.Namespace)
	if err != nil {
		return err
	}

	for _, item := range items {
		val, err := clt.GetUsageFromUnstructured(item, instance.Spec.Source.Path)
		if err != nil {
			r.log.Error(err, "Error getting usage from unstructured while updating CustomQuota usage")

			continue
		}

		quant, err := resource.ParseQuantity(val)
		if err != nil {
			r.log.Error(err, "Error parsing quantity while updating CustomQuota usage")

			continue
		}

		instance.Status.Usage.Used.Add(quant)

		instance.Status.Claims = append(instance.Status.Claims, meta.NamespacedObjectWithUIDReference{
			Name:      item.GetName(),
			Namespace: meta.RFC1123SubdomainName(item.GetNamespace()),
			UID:       types.UID(item.GetUID()),
		})
	}

	// Calculate Delta
	instance.Status.Usage.Available = instance.Spec.Limit
	instance.Status.Usage.Available.Sub(instance.Status.Usage.Used)

	return nil
}

func (r *customQuotaClaimController) emitMetrics(
	instance *capsulev1beta2.CustomQuota,
) {

	// Condition Metrics
	for _, status := range []string{meta.ReadyCondition} {
		var value float64

		cond := instance.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.metrics.DeleteConditionMetricByType(instance.GetName(), instance.GetNamespace(), status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.metrics.ConditionGauge.WithLabelValues(instance.GetName(), instance.GetNamespace(), status).Set(value)
	}

	// Usage Metrics
	r.metrics.ResourceUsageGauge.WithLabelValues(instance.GetName(), instance.GetNamespace()).Set(float64(instance.Status.Usage.Used.MilliValue()) / 1000)
	r.metrics.ResourceUsageGauge.WithLabelValues(instance.GetName(), instance.GetNamespace()).Set(float64(instance.Status.Usage.Available.MilliValue()) / 1000)
	r.metrics.ResourceUsageGauge.WithLabelValues(instance.GetName(), instance.GetNamespace()).Set(float64(instance.Spec.Limit.MilliValue()) / 1000)
}

func (r *customQuotaClaimController) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.CustomQuota,
	reconcileError error,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.CustomQuota{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
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

		return r.Client.Status().Update(ctx, latest)
	})
}

func hasClaimUID(claims []meta.NamespacedObjectWithUIDReference, uid types.UID) bool {
	for i := range claims {
		if claims[i].UID == uid {
			return true
		}
	}
	return false
}

func (r *customQuotaClaimController) hasConsistentStateWithCache(instance *capsulev1beta2.CustomQuota) bool {
	return r.hasConsistentReservedUsage(instance) &&
		r.hasConsistentPendingDeletes(instance)
}

func (r *customQuotaClaimController) hasConsistentReservedUsage(instance *capsulev1beta2.CustomQuota) bool {
	if r.cache == nil {
		return true
	}

	entry, ok := r.cache.Get(MakeCustomQuotaCacheKey(*instance))
	if !ok || entry.Reserved.IsZero() {
		return true
	}

	return instance.Status.Usage.Used.Cmp(entry.Reserved) >= 0
}

func (r *customQuotaClaimController) hasConsistentPendingDeletes(instance *capsulev1beta2.CustomQuota) bool {
	if r.cache == nil {
		return true
	}

	entry, ok := r.cache.Get(MakeCustomQuotaCacheKey(*instance))
	if !ok || len(entry.PendingDeletes) == 0 {
		return true
	}

	for _, hint := range entry.PendingDeletes {
		if hasClaimUID(instance.Status.Claims, hint.UID) {
			return false
		}
	}

	return true
}
