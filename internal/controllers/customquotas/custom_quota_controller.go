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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

type customQuotaClaimController struct {
	client.Client

	log      logr.Logger
	recorder events.EventRecorder
	metrics  *metrics.CustomQuotaRecorder
	mapper   k8smeta.RESTMapper

	jsonPathCache *cache.JSONPathCache
	targetsCache  *cache.CompiledTargetsCache[string]
}

func (r *customQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.mapper = mgr.GetRESTMapper()

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
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")
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
		return reconcile.Result{}, err
	}

	reconcileErr := r.reconcile(ctx, log, instance)

	if err := r.updateStatus(ctx, instance, reconcileErr); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot update status: %w", err)
	}

	r.emitMetrics(instance)

	if err := patchHelper.Patch(ctx, instance); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(fmt.Errorf("failed to patch claim: %w", err))
	}

	if reconcileErr != nil {
		return reconcile.Result{}, reconcileErr
	}

	requeueAfter, err := r.reconcileLedger(ctx, log, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	if requeueAfter != nil {
		log.V(3).Info("ledger still has pending work, requeueing",
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
	instance.Status.Targets = []capsulev1beta2.CustomQuotaStatusTarget{}
	instance.Status.Usage = capsulev1beta2.CustomQuotaStatusUsage{}
	instance.Status.Claims = nil

	for _, src := range instance.Spec.Sources {
		kind := schema.GroupVersionKind{
			Group:   src.Group,
			Version: src.Version,
			Kind:    src.Kind,
		}

		mapping, err := r.mapper.RESTMapping(kind.GroupKind(), kind.Version)
		if err != nil {
			return fmt.Errorf("failed to resolve REST mapping for %s: %w", kind.String(), err)
		}

		if mapping.Scope.Name() != k8smeta.RESTScopeNameNamespace {
			return fmt.Errorf("GVK %s is not namespaced", kind.String())
		}

		instance.Status.Targets = append(instance.Status.Targets, capsulev1beta2.CustomQuotaStatusTarget{
			CustomQuotaSpecSource: src,
			Scope:                 mapping.Scope.Name(),
		})
	}

	var errs []error

	seenClaims := make(map[types.UID]struct{})
	itemsByGVK := make(map[schema.GroupVersionKind][]unstructured.Unstructured, len(instance.Status.Targets))

	targets, err := CompileTargets(r.jsonPathCache, instance.Status.Targets)
	if err != nil {
		return err
	}

	r.targetsCache.Set(MakeCustomQuotaCacheKey(instance.GetNamespace(), instance.GetName()), targets)

	for _, target := range targets {
		gvk := schema.GroupVersionKind{
			Group:   target.Group,
			Version: target.Version,
			Kind:    target.Kind,
		}

		items, ok := itemsByGVK[gvk]
		if !ok {
			items, err = getResourcesByGVK(ctx, gvk, r.Client, instance.Spec.ScopeSelectors, instance.Namespace)
			if err != nil {
				errs = append(errs, fmt.Errorf("list resources for %s: %w", gvk.String(), err))

				continue
			}

			itemsByGVK[gvk] = items
		}

		log.V(3).Info("listed resources for target",
			"gvk", gvk.String(),
			"count", len(items),
			"namespace", instance.Namespace,
			"scopeSelectors", instance.Spec.ScopeSelectors,
		)

		for _, item := range items {
			matches, err := MatchesCompiledSelectorsWithFields(item, target.CompiledSelectors)
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"evaluate selectors for %s/%s (%s): %w",
					item.GetNamespace(),
					item.GetName(),
					item.GetObjectKind().GroupVersionKind().String(),
					err,
				))

				continue
			}

			if !matches {
				continue
			}

			var usage resource.Quantity

			switch target.Operation {
			case quota.OpCount:
				usage = *resource.NewQuantity(1, resource.DecimalSI)

			case quota.OpAdd, quota.OpSub:
				usage, err = quota.ParseQuantityFromUnstructured(item, target.CompiledPath)
				if err != nil {
					errs = append(errs, fmt.Errorf(
						"get usage from %s/%s (%s) path %q op %q: %w",
						item.GetNamespace(),
						item.GetName(),
						item.GetObjectKind().GroupVersionKind().String(),
						target.Path,
						target.Operation,
						err,
					))

					continue
				}

			default:
				errs = append(errs, fmt.Errorf(
					"unsupported operation %q for %s/%s (%s)",
					target.Operation,
					item.GetNamespace(),
					item.GetName(),
					item.GetObjectKind().GroupVersionKind().String(),
				))

				continue
			}

			switch target.Operation {
			case quota.OpSub:
				usage.Neg()
				instance.Status.Usage.Used.Add(usage)
				quota.ClampQuantityToZero(&instance.Status.Usage.Used)
			default:
				instance.Status.Usage.Used.Add(usage)
			}

			uid := item.GetUID()
			if _, exists := seenClaims[uid]; !exists {
				seenClaims[uid] = struct{}{}

				instance.Status.Claims = append(instance.Status.Claims, capsulev1beta2.CustomQuotaClaimItem{
					GroupVersionKind: metav1.GroupVersionKind(item.GroupVersionKind()),
					NamespacedObjectWithUIDReference: meta.NamespacedObjectWithUIDReference{
						Name:      item.GetName(),
						Namespace: meta.RFC1123SubdomainName(item.GetNamespace()),
						UID:       uid,
					},
					Usage: usage,
				})
			}
		}
	}

	if instance.Status.Usage.Used.Sign() < 0 {
		instance.Status.Usage.Used = resource.MustParse("0")
	}

	instance.Status.Usage.Available = instance.Spec.Limit.DeepCopy()
	instance.Status.Usage.Available.Sub(instance.Status.Usage.Used)

	if instance.Status.Usage.Available.Sign() < 0 {
		instance.Status.Usage.Available = resource.MustParse("0")
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (r *customQuotaClaimController) applyActiveLedgerReservationsToStatus(
	ctx context.Context,
	instance *capsulev1beta2.CustomQuota,
) error {
	key := types.NamespacedName{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
	}

	ledger := &capsulev1beta2.QuantityLedger{}
	if err := r.Get(ctx, key, ledger); err != nil {
		if apierrors.IsNotFound(err) {
			instance.Status.Usage.Available = instance.Spec.Limit.DeepCopy()
			instance.Status.Usage.Available.Sub(instance.Status.Usage.Used)
			if instance.Status.Usage.Available.Sign() < 0 {
				instance.Status.Usage.Available = resource.MustParse("0")
			}
			return nil
		}
		return err
	}

	now := metav1.Now()

	seenUIDs := make(map[types.UID]struct{}, len(instance.Status.Claims))
	for _, claim := range instance.Status.Claims {
		if claim.UID != "" {
			seenUIDs[claim.UID] = struct{}{}
		}
	}

	for _, res := range ledger.Status.Reservations {
		if res.ExpiresAt != nil && res.ExpiresAt.Before(&now) {
			continue
		}

		if reservationMaterializedLedger(res, instance.Status.Claims) {
			continue
		}

		instance.Status.Usage.Used.Add(res.Usage)

		if res.ObjectRef.UID != "" {
			if _, exists := seenUIDs[res.ObjectRef.UID]; exists {
				continue
			}
			seenUIDs[res.ObjectRef.UID] = struct{}{}
		}

		instance.Status.Claims = append(instance.Status.Claims, capsulev1beta2.CustomQuotaClaimItem{
			GroupVersionKind: metav1.GroupVersionKind{
				Group:   res.ObjectRef.APIGroup,
				Version: res.ObjectRef.APIVersion,
				Kind:    res.ObjectRef.Kind,
			},
			NamespacedObjectWithUIDReference: meta.NamespacedObjectWithUIDReference{
				Name:      res.ObjectRef.Name,
				Namespace: meta.RFC1123SubdomainName(res.ObjectRef.Namespace),
				UID:       res.ObjectRef.UID,
			},
			Usage: res.Usage.DeepCopy(),
		})
	}

	if instance.Status.Usage.Used.Sign() < 0 {
		instance.Status.Usage.Used = resource.MustParse("0")
	}

	instance.Status.Usage.Available = instance.Spec.Limit.DeepCopy()
	instance.Status.Usage.Available.Sub(instance.Status.Usage.Used)
	if instance.Status.Usage.Available.Sign() < 0 {
		instance.Status.Usage.Available = resource.MustParse("0")
	}

	return nil
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

	var requeueAfter *time.Duration

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := r.Get(ctx, key, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		now := metav1.Now()
		pendingDeleteTTL := 30 * time.Second

		activeReservations := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		for _, res := range ledger.Status.Reservations {
			materialized := reservationMaterializedLedger(res, instance.Status.Claims)
			expired := res.ExpiresAt != nil && res.ExpiresAt.Before(&now)

			log.V(3).Info("evaluating ledger reservation",
				"ledger", key.String(),
				"reservationID", res.ID,
				"usage", res.Usage.String(),
				"uid", string(res.ObjectRef.UID),
				"group", res.ObjectRef.APIGroup,
				"version", res.ObjectRef.APIVersion,
				"kind", res.ObjectRef.Kind,
				"namespace", res.ObjectRef.Namespace,
				"name", res.ObjectRef.Name,
				"materialized", materialized,
				"expired", expired,
			)

			if materialized || expired {
				continue
			}

			activeReservations = append(activeReservations, res)

			if res.ExpiresAt != nil {
				requeueAfter = minDurationPtr(requeueAfter, time.Until(res.ExpiresAt.Time))
			}
		}

		activeDeletes := make([]capsulev1beta2.QuantityLedgerPendingDelete, 0, len(ledger.Status.PendingDeletes))
		for _, pd := range ledger.Status.PendingDeletes {
			stillPresent := pendingDeleteStillPresent(pd, instance.Status.Claims)
			expired := now.Sub(pd.CreatedAt.Time) >= pendingDeleteTTL

			log.V(3).Info("evaluating pending delete",
				"ledger", key.String(),
				"uid", string(pd.ObjectRef.UID),
				"group", pd.ObjectRef.APIGroup,
				"version", pd.ObjectRef.APIVersion,
				"kind", pd.ObjectRef.Kind,
				"namespace", pd.ObjectRef.Namespace,
				"name", pd.ObjectRef.Name,
				"stillPresent", stillPresent,
				"expired", expired,
			)

			switch {
			case stillPresent:
				activeDeletes = append(activeDeletes, pd)
				requeueAfter = minDurationPtr(requeueAfter, immediatePendingDeleteRequeue)
			case expired:
				// drop stale hint
			default:
				// object is no longer collected by the controller; remove hint now
			}
		}

		ledger.Status.Reservations = activeReservations
		ledger.Status.PendingDeletes = activeDeletes
		ledger.Status.Reserved = resource.MustParse("0")
		for _, res := range activeReservations {
			ledger.Status.Reserved.Add(res.Usage)
		}

		return r.Status().Update(ctx, ledger)
	})
	if err != nil {
		return nil, err
	}

	return requeueAfter, nil
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

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		instance.Status = latest.Status

		return nil
	})
}
