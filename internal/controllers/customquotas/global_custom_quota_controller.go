// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

type clusterCustomQuotaClaimController struct {
	client.Client

	log      logr.Logger
	recorder events.EventRecorder
	metrics  *metrics.GlobalCustomQuotaRecorder
	mapper   k8smeta.RESTMapper

	jsonPathCache *cache.JSONPathCache
	targetsCache  *cache.CompiledTargetsCache[string]
}

func (r *clusterCustomQuotaClaimController) SetupWithManager(mgr ctrl.Manager, cfg utils.ControllerOptions) error {
	r.mapper = mgr.GetRESTMapper()

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.GlobalCustomQuota{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.ReconcileRequestedPredicate{},
				),
			),
		).
		Watches(
			&capsulev1beta2.QuantityLedger{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.GlobalCustomQuota{}),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToGlobalCustomQuotas),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					if e.ObjectOld == nil || e.ObjectNew == nil {
						return false
					}

					oldNs, okOld := e.ObjectOld.(*corev1.Namespace)
					newNs, okNew := e.ObjectNew.(*corev1.Namespace)
					if !okOld || !okNew {
						return false
					}

					return !reflect.DeepEqual(oldNs.Labels, newNs.Labels) ||
						!reflect.DeepEqual(oldNs.Annotations, newNs.Annotations)
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *clusterCustomQuotaClaimController) mapNamespaceToGlobalCustomQuotas(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil
	}

	var quotaList capsulev1beta2.GlobalCustomQuotaList
	if err := r.List(ctx, &quotaList); err != nil {
		r.log.Error(err, "cannot list GlobalCustomQuota objects for namespace event", "namespace", ns.Name)
		return nil
	}

	requests := make([]reconcile.Request, 0, len(quotaList.Items))

	for i := range quotaList.Items {
		gcq := &quotaList.Items[i]

		if shouldReconcileForNamespaceEvent(gcq, ns.Name) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: gcq.Name,
				},
			})
		}
	}

	return requests
}

func shouldReconcileForNamespaceEvent(
	instance *capsulev1beta2.GlobalCustomQuota,
	namespace string,
) bool {
	if len(instance.Spec.NamespaceSelectors) > 0 {
		return true
	}

	for _, ns := range instance.Status.Namespaces {
		if ns == namespace {
			return true
		}
	}

	return false
}

func (r *clusterCustomQuotaClaimController) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.GlobalCustomQuota{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")
			r.metrics.DeleteAllMetricsForGlobalCustomQuota(request.Name)
			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")
		return reconcile.Result{}, err
	}

	defer func() {
		r.emitMetrics(instance)
	}()

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	if err := r.ensureQuotaLedger(ctx, instance); err != nil {
		return reconcile.Result{}, err
	}

	reconcileErr := r.reconcile(ctx, log, instance)

	// Normalize ledger state against freshly rebuilt claims first.
	requeueAfter, ledgerErr := r.reconcileLedger(ctx, log, instance)
	if ledgerErr != nil {
		return reconcile.Result{}, ledgerErr
	}

	if err := r.updateStatus(ctx, instance, reconcileErr); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot update status: %w", err)
	}

	if err := patchHelper.Patch(ctx, instance); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(fmt.Errorf("failed to patch claim: %w", err))
	}

	if reconcileErr != nil {
		return reconcile.Result{}, reconcileErr
	}

	if requeueAfter != nil {
		log.V(3).Info("ledger still has pending work, requeueing",
			"customQuota", instance.Name,
		)

		return ctrl.Result{RequeueAfter: *requeueAfter}, nil
	}

	return ctrl.Result{}, nil
}
func (r *clusterCustomQuotaClaimController) reconcile(
	ctx context.Context,
	log logr.Logger,
	instance *capsulev1beta2.GlobalCustomQuota,
) error {
	instance.Status.Targets = []capsulev1beta2.CustomQuotaStatusTarget{}
	instance.Status.Usage = capsulev1beta2.CustomQuotaStatusUsage{}
	instance.Status.Claims = nil

	for _, src := range instance.Spec.Sources {
		kind := schema.GroupVersionKind{
			Group:   src.GroupVersionKind.Group,
			Version: src.GroupVersionKind.Version,
			Kind:    src.GroupVersionKind.Kind,
		}

		mapping, err := r.mapper.RESTMapping(kind.GroupKind(), kind.Version)
		if err != nil {
			return fmt.Errorf("failed to resolve REST mapping for %s: %w", kind.String(), err)
		}

		instance.Status.Targets = append(instance.Status.Targets, capsulev1beta2.CustomQuotaStatusTarget{
			CustomQuotaSpecSource: src,
			Scope:                 mapping.Scope.Name(),
		})
	}

	var namespaces []string
	var err error

	if len(instance.Spec.NamespaceSelectors) > 0 {
		namespaces, err = selectors.GetNamespacesMatchingSelectorsStrings(
			ctx,
			r.Client,
			instance.Spec.NamespaceSelectors,
		)
		if err != nil {
			return err
		}
	} else {
		namespaces = []string{"*"}
	}

	instance.Status.Namespaces = namespaces

	var errs []error
	seenClaims := make(map[types.UID]struct{})
	itemsByGVK := make(map[schema.GroupVersionKind][]unstructured.Unstructured, len(instance.Status.Targets))

	targets, err := CompileTargets(r.jsonPathCache, instance.Status.Targets)
	if err != nil {
		return err
	}

	r.targetsCache.Set(MakeGlobalCustomQuotaCacheKey(instance.GetName()), targets)

	for _, target := range targets {
		gvk := schema.GroupVersionKind{
			Group:   target.GroupVersionKind.Group,
			Version: target.GroupVersionKind.Version,
			Kind:    target.GroupVersionKind.Kind,
		}

		items, ok := itemsByGVK[gvk]
		if !ok {
			items, err = getResourcesByGVK(ctx, gvk, r.Client, instance.Spec.ScopeSelectors, namespaces...)
			if err != nil {
				errs = append(errs, fmt.Errorf("list resources for %s: %w", gvk.String(), err))
				continue
			}
			itemsByGVK[gvk] = items
		}

		log.V(3).Info("listed resources for target",
			"gvk", gvk.String(),
			"count", len(items),
			"namespaces", namespaces,
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

			uid := types.UID(item.GetUID())
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

func (r *clusterCustomQuotaClaimController) reconcileLedger(
	ctx context.Context,
	log logr.Logger,
	instance *capsulev1beta2.GlobalCustomQuota,
) (*time.Duration, error) {
	key := types.NamespacedName{
		Name:      instance.GetName(),
		Namespace: configuration.ControllerNamespace(),
	}

	var requeueAfter *time.Duration

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
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

			// NEW: schedule requeue on reservation expiry
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

		if err := r.Status().Update(ctx, ledger); err != nil {
			return err
		}

		seenUIDs := make(map[types.UID]struct{}, len(instance.Status.Claims))
		for _, claim := range instance.Status.Claims {
			if claim.UID != "" {
				seenUIDs[claim.UID] = struct{}{}
			}
		}

		for _, res := range activeReservations {
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
	})

	if err != nil {
		return nil, err
	}

	return requeueAfter, nil
}

func (r *clusterCustomQuotaClaimController) ensureQuotaLedger(
	ctx context.Context,
	instance *capsulev1beta2.GlobalCustomQuota,
) error {
	ledger := &capsulev1beta2.QuantityLedger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.GetName(),
			Namespace: configuration.ControllerNamespace(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ledger, func() error {
		if ledger.Labels == nil {
			ledger.Labels = map[string]string{}
		}

		ledger.Labels[meta.ManagedByCapsuleLabel] = meta.ValueController

		ledger.Spec.TargetRef = capsulev1beta2.QuantityLedgerTargetRef{
			APIGroup: capsulev1beta2.GroupVersion.Group,
			Kind:     "GlobalCustomQuota",
			Name:     instance.GetName(),
			UID:      instance.GetUID(),
		}

		return controllerutil.SetControllerReference(instance, ledger, r.Scheme())
	})
	if err != nil {
		return fmt.Errorf("create or update QuantityLedger %s/%s for GlobalCustomQuota %s: %w",
			ledger.Namespace, ledger.Name, instance.GetName(), err)
	}

	return nil
}

func (r *clusterCustomQuotaClaimController) emitMetrics(
	instance *capsulev1beta2.GlobalCustomQuota,
) {
	// Condition Metrics
	for _, status := range []string{meta.ReadyCondition} {
		var value float64

		cond := instance.Status.Conditions.GetConditionByType(status)
		if cond == nil {
			r.metrics.DeleteConditionMetricByType(instance.GetName(), status)

			continue
		}

		if cond.Status == metav1.ConditionTrue {
			value = 1
		}

		r.metrics.ConditionGauge.WithLabelValues(instance.GetName(), status).Set(value)
	}

	// Usage Metrics
	r.metrics.ResourceUsageGauge.WithLabelValues(instance.GetName()).Set(float64(instance.Status.Usage.Used.MilliValue()) / 1000)
	r.metrics.ResourceAvailableGauge.WithLabelValues(instance.GetName()).Set(float64(instance.Status.Usage.Available.MilliValue()) / 1000)
	r.metrics.ResourceLimitGauge.WithLabelValues(instance.GetName()).Set(float64(instance.Spec.Limit.MilliValue()) / 1000)

	// Emit for Claims
	r.metrics.ResourceItemUsageGauge.DeletePartialMatch(map[string]string{
		"custom_quota": instance.GetName(),
	})

	// Skip emitting metrics on claim basis
	if !instance.Spec.Options.EmitPerClaimMetrics {
		return
	}

	for _, claim := range instance.Status.Claims {
		r.metrics.ResourceItemUsageGauge.WithLabelValues(
			instance.GetName(),
			claim.Name,
			string(claim.Namespace),
			claim.Kind,
			claim.Group,
		).Set(float64(claim.Usage.MilliValue()) / 1000)
	}
}

func (r *clusterCustomQuotaClaimController) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.GlobalCustomQuota,
	reconcileError error,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalCustomQuota{}
		if err = r.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
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

func pendingDeleteStillPresent(
	pd capsulev1beta2.QuantityLedgerPendingDelete,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) bool {
	for _, claim := range claims {
		if pd.ObjectRef.UID != "" && claim.UID != "" && pd.ObjectRef.UID == claim.UID {
			return true
		}

		if pd.ObjectRef.APIGroup == claim.Group &&
			pd.ObjectRef.APIVersion == claim.Version &&
			pd.ObjectRef.Kind == claim.Kind &&
			pd.ObjectRef.Namespace == string(claim.Namespace) &&
			pd.ObjectRef.Name == claim.Name {
			return true
		}
	}

	return false
}

func reservationMaterializedLedger(
	res capsulev1beta2.QuantityLedgerReservation,
	claims []capsulev1beta2.CustomQuotaClaimItem,
) bool {
	for _, claim := range claims {
		if res.ObjectRef.UID != "" && claim.UID != "" && res.ObjectRef.UID == claim.UID {
			return true
		}

		if res.ObjectRef.APIGroup == claim.Group &&
			res.ObjectRef.APIVersion == claim.Version &&
			res.ObjectRef.Kind == claim.Kind &&
			res.ObjectRef.Namespace == string(claim.Namespace) &&
			res.ObjectRef.Name == claim.Name {
			return true
		}
	}
	return false
}
