// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	cutils "github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type customQuotaClaimController struct {
	client.Client

	reader client.Reader

	log      logr.Logger
	recorder events.EventRecorder
	metrics  *metrics.CustomQuotaRecorder
	mapper   k8smeta.RESTMapper

	jsonPathCache *cache.JSONPathCache
	targetsCache  *cache.CompiledTargetsCache[string]
}

func (r *customQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg cutils.ControllerOptions) error {
	r.mapper = mgr.GetRESTMapper()
	r.reader = mgr.GetAPIReader()

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.CustomQuota{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.ReconcileRequestedPredicate{},
				),
			),
		).
		Watches(
			&capsulev1beta2.QuantityLedger{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&capsulev1beta2.CustomQuota{},
			),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *customQuotaClaimController) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("Request.Name", request.Name, "Request.Namespace", request.Namespace)

	instance := &capsulev1beta2.CustomQuota{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(5).Info("Request object not found, could have been deleted after reconcile request")
			r.metrics.DeleteAllMetricsForCustomQuota(request.Name, request.Namespace)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return reconcile.Result{}, err
	}

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	if err := r.ensureQuotaLedger(ctx, instance); err != nil {
		if instance.DeletionTimestamp != nil || shouldIgnoreLedgerEnsureError(err) {
			log.V(4).Info("skipping QuantityLedger ensure because CustomQuota or namespace is terminating",
				"customQuota", request.String(),
				"error", err,
			)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	reconcileErr := r.reconcile(ctx, log, instance)

	requeueAfter, ledgerErr := r.reconcileLedger(ctx, log, instance)

	statusErr := errors.Join(reconcileErr, ledgerErr)

	if err := r.updateStatus(ctx, instance, statusErr); err != nil {
		if caperrors.IgnoreGone(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("cannot update status: %w", err)
	}

	r.emitMetrics(instance)

	if err := patchHelper.Patch(ctx, instance); err != nil {
		if caperrors.IgnoreGone(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, fmt.Errorf("cannot patch: %w", err)
	}

	if requeueAfter != nil {
		log.V(5).Info("ledger still has pending work, requeueing",
			"customQuota", instance.Name,
			"namespace", instance.Namespace,
			"after", requeueAfter.String(),
		)

		return ctrl.Result{RequeueAfter: *requeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func (r *customQuotaClaimController) reconcile(
	ctx context.Context,
	log logr.Logger,
	instance *capsulev1beta2.CustomQuota,
) error {
	result, err := reconcileQuotaUsage(ctx, quotaUsageReconcileInput{
		Log: log,

		Client: r.Client,
		Mapper: r.mapper,

		JSONPathCache: r.jsonPathCache,

		Sources:        instance.Spec.Sources,
		ScopeSelectors: instance.Spec.ScopeSelectors,

		Namespaces: []string{instance.Namespace},

		RequireNamespacedTargets: true,

		CacheKey:     MakeCustomQuotaCacheKey(instance.GetNamespace(), instance.GetName()),
		TargetsCache: r.targetsCache,
	}, instance.Spec.Limit)

	instance.Status.Targets = result.Targets
	instance.Status.Usage = result.Usage
	instance.Status.Claims = result.Claims

	return err
}

func (r *customQuotaClaimController) reconcileLedger(
	ctx context.Context,
	log logr.Logger,
	instance *capsulev1beta2.CustomQuota,
) (*time.Duration, error) {
	key := types.NamespacedName{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
	}

	return reconcileQuantityLedgerAllocation(
		ctx,
		r.Client,
		log,
		key,
		instance.Status.Usage.Used.DeepCopy(),
		instance.Status.Claims,
	)
}

func (r *customQuotaClaimController) ensureQuotaLedger(
	ctx context.Context,
	instance *capsulev1beta2.CustomQuota,
) error {
	ledger := &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.GetName(),
			Namespace: instance.GetNamespace(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ledger, func() error {
		if ledger.Labels == nil {
			ledger.Labels = map[string]string{}
		}

		ledger.Labels[meta.ManagedByCapsuleLabel] = meta.ValueController

		ledger.Spec.TargetRef = capsulev1beta2.QuantityLedgerTargetRef{
			APIGroup:  capsulev1beta2.GroupVersion.Group,
			Kind:      "CustomQuota",
			Namespace: instance.GetNamespace(),
			Name:      instance.GetName(),
			UID:       instance.GetUID(),
		}

		return controllerutil.SetControllerReference(instance, ledger, r.Scheme())
	})
	if err != nil {
		return fmt.Errorf("create or update QuantityLedger %s/%s for CustomQuota %s/%s: %w",
			ledger.Namespace, ledger.Name, instance.Namespace, instance.Name, err)
	}

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
	r.metrics.ResourceAvailableGauge.WithLabelValues(instance.GetName(), instance.GetNamespace()).Set(float64(instance.Status.Usage.Available.MilliValue()) / 1000)
	r.metrics.ResourceLimitGauge.WithLabelValues(instance.GetName(), instance.GetNamespace()).Set(float64(instance.Spec.Limit.MilliValue()) / 1000)

	// Emit for Claims
	r.metrics.ResourceItemUsageGauge.DeletePartialMatch(map[string]string{
		"custom_quota":     instance.GetName(),
		"target_namespace": instance.GetNamespace(),
	})

	// Skip emitting metrics on claim basis
	if !instance.Spec.Options.EmitPerClaimMetrics {
		return
	}

	for _, claim := range instance.Status.Claims {
		r.metrics.ResourceItemUsageGauge.WithLabelValues(
			instance.GetName(),
			instance.GetNamespace(),
			claim.Name,
			claim.Kind,
			claim.Group,
		).Set(float64(claim.Usage.MilliValue()) / 1000)
	}
}

func (r *customQuotaClaimController) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.CustomQuota,
	reconcileError error,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.CustomQuota{}
		if err = r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		latest.Status = instance.Status
		latest.Status.ObservedGeneration = instance.GetGeneration()

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		instance.Status = latest.Status

		return nil
	})
}
