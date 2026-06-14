// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	controller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	index "github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

// Might need some tuning in the.
var customAdmissionBackoff = wait.Backoff{
	Steps:    6,
	Duration: 20 * time.Millisecond,
	Factor:   1.5,
	Jitter:   0.2,
}

var ledgerMutationBackoff = wait.Backoff{
	Steps:    8,
	Duration: 10 * time.Millisecond,
	Factor:   1.6,
	Jitter:   0.2,
}

type objectCalculationHandler struct {
	targetsCache  *cache.CompiledTargetsCache[string]
	jsonPathCache *cache.JSONPathCache
}

func ObjectCalculationHandler(
	targetsCache *cache.CompiledTargetsCache[string],
	jsonPathCache *cache.JSONPathCache,
) handlers.Handler {
	return &objectCalculationHandler{
		targetsCache:  targetsCache,
		jsonPathCache: jsonPathCache,
	}
}

func (h *objectCalculationHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		log := log.FromContext(ctx).WithValues(
			"op", "create",
			"kind", req.Kind.String(),
			"namespace", req.Namespace,
			"requestUID", string(req.UID),
			"name", req.Name,
		)

		u, err := getUnstructured(req.Object)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		var finalResp *admission.Response

		err = retry.OnError(customAdmissionBackoff, apierrors.IsConflict, func() error {
			matched, err := h.matchAllQuotas(ctx, c, req, u)
			if err != nil {
				finalResp = ad.ErroredResponse(err)

				return nil
			}

			if len(matched) == 0 {
				return nil
			}

			evaluated, err := h.evaluateMatchedQuotas(ctx, u, matched)
			if err != nil {
				finalResp = ad.Denyf(
					"creating resource %s/%s (%s) cannot be admitted because custom quota usage could not be calculated: %v",
					req.Namespace,
					req.Name,
					req.Kind.String(),
					err,
				)

				return nil
			}

			type appliedReservation struct {
				LedgerKey     types.NamespacedName
				ReservationID string
			}

			applied := make([]appliedReservation, 0, len(evaluated))

			for _, item := range evaluated {
				ledgerKey := quantityLedgerKeyForMatchedQuota(item)

				reservation := buildReservation(req, u, item.Usage, item.Key)

				allowed, effectiveUsed, reserved, err := reserveCreateOnLedger(
					ctx,
					c,
					reader,
					item,
					&reservation,
				)
				if err != nil {
					for _, a := range applied {
						_ = deleteLedgerReservation(ctx, c, reader, a.LedgerKey, a.ReservationID)
					}

					return err
				}

				if !allowed {
					for _, a := range applied {
						_ = deleteLedgerReservation(ctx, c, reader, a.LedgerKey, a.ReservationID)
					}

					available := item.Limit.DeepCopy()
					available.Sub(effectiveUsed)

					if available.Sign() < 0 {
						available = resource.MustParse("0")
					}

					log.V(5).Info("denying create due to quota",
						"quotaKey", item.Key,
						"quotaName", item.Name,
						"isGlobal", item.IsGlobal,
						"requestedUsage", item.Usage.String(),
						"currentUsed", effectiveUsed.String(),
						"available", available.String(),
						"limit", item.Limit.String(),
						"inflightReserved", reserved.String(),
					)

					finalResp = ad.Denyf(
						"creating resource exceeds limit for %s %q (requested=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
						quotaTypeName(item.IsGlobal),
						item.Name,
						item.Usage.String(),
						effectiveUsed.String(),
						available.String(),
						item.Limit.String(),
						reserved.String(),
					)

					return nil
				}

				applied = append(applied, appliedReservation{
					LedgerKey:     ledgerKey,
					ReservationID: reservation.ID,
				})
			}

			finalResp = nil

			return nil
		})
		if err != nil {
			if apierrors.IsConflict(err) {
				return ad.Denyf(
					"custom quota admission could not reserve usage due to concurrent quota updates after %d attempts; please retry the request: %v",
					customAdmissionBackoff.Steps,
					err,
				)
			}

			return ad.ErroredResponse(err)
		}

		return finalResp
	}
}

//nolint:gocognit,cyclop,maintidx
func (h *objectCalculationHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldObj, err := getUnstructured(req.OldObject)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newObj, err := getUnstructured(req.Object)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		var finalResp *admission.Response

		err = retry.OnError(customAdmissionBackoff, apierrors.IsConflict, func() error {
			oldMatched, err := h.matchAllQuotas(ctx, c, req, oldObj)
			if err != nil {
				finalResp = ad.ErroredResponse(err)

				return nil
			}

			newMatched, err := h.matchAllQuotas(ctx, c, req, newObj)
			if err != nil {
				finalResp = ad.ErroredResponse(err)

				return nil
			}

			oldEvaluated, err := h.evaluateMatchedQuotas(ctx, oldObj, oldMatched)
			if err != nil {
				finalResp = ad.Denyf(
					"updating resource %s/%s (%s) cannot be admitted because previous custom quota usage could not be calculated: %v",
					req.Namespace,
					req.Name,
					req.Kind.String(),
					err,
				)

				return nil
			}

			newEvaluated, err := h.evaluateMatchedQuotas(ctx, newObj, newMatched)
			if err != nil {
				finalResp = ad.Denyf(
					"updating resource %s/%s (%s) cannot be admitted because new custom quota usage could not be calculated: %v",
					req.Namespace,
					req.Name,
					req.Kind.String(),
					err,
				)

				return nil
			}

			oldByKey := evaluatedByKey(oldEvaluated)
			newByKey := evaluatedByKey(newEvaluated)

			relevantChange := meta.LabelsChangedUnstructured(oldObj, newObj) || len(oldByKey) != len(newByKey)
			if !relevantChange {
				for key, oldItem := range oldByKey {
					newItem, ok := newByKey[key]
					if !ok || oldItem.Usage.Cmp(newItem.Usage) != 0 {
						relevantChange = true

						break
					}
				}
			}

			if !relevantChange {
				finalResp = nil

				return nil
			}

			type appliedUpdate struct {
				LedgerKey     types.NamespacedName
				ReservationID string
				OldUsage      resource.Quantity
				NewUsage      resource.Quantity
			}

			applied := make([]appliedUpdate, 0, len(oldByKey)+len(newByKey))

			for _, key := range allKeys(oldByKey, newByKey) {
				oldItem, hadOld := oldByKey[key]
				newItem, hadNew := newByKey[key]

				var base evaluatedQuota

				switch {
				case hadNew:
					base = newItem
				case hadOld:
					base = oldItem
				default:
					continue
				}

				oldUsage := resource.MustParse("0")
				if hadOld {
					oldUsage = oldItem.Usage.DeepCopy()
				}

				newUsage := resource.MustParse("0")
				if hadNew {
					newUsage = newItem.Usage.DeepCopy()
				}

				ledgerKey := quantityLedgerKeyForMatchedQuota(base)

				var pendingDelete *capsulev1beta2.QuantityLedgerObjectRef
				if hadOld {
					pendingDelete = &capsulev1beta2.QuantityLedgerObjectRef{
						APIGroup:   req.Kind.Group,
						APIVersion: req.Kind.Version,
						Kind:       req.Kind.Kind,
						Namespace:  oldObj.GetNamespace(),
						Name:       oldObj.GetName(),
						UID:        oldObj.GetUID(),
					}
				}

				var reservation *capsulev1beta2.QuantityLedgerReservation

				if hadNew && newUsage.Sign() > 0 {
					r := buildReservation(req, newObj, newUsage, base.Key)
					reservation = &r
				}

				allowed, effectiveUsed, reserved, err := replaceUsageOnLedger(
					ctx,
					c,
					reader,
					base,
					oldUsage,
					newUsage,
					reservation,
					pendingDelete,
				)
				if err != nil {
					for _, v := range slices.Backward(applied) {
						_ = rollbackUsageReplacementOnLedger(
							ctx,
							c,
							reader,
							v.LedgerKey,
							v.ReservationID,
							v.OldUsage,
							v.NewUsage,
						)
					}

					return err
				}

				if !allowed {
					for _, v := range slices.Backward(applied) {
						_ = rollbackUsageReplacementOnLedger(
							ctx,
							c,
							reader,
							v.LedgerKey,
							v.ReservationID,
							v.OldUsage,
							v.NewUsage,
						)
					}

					available := base.Limit.DeepCopy()
					available.Sub(effectiveUsed)

					if available.Sign() < 0 {
						available = resource.MustParse("0")
					}

					finalResp = ad.Denyf(
						"updating resource exceeds limit for %s %q (requested=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
						quotaTypeName(base.IsGlobal),
						base.Name,
						newUsage.String(),
						effectiveUsed.String(),
						available.String(),
						base.Limit.String(),
						reserved.String(),
					)

					return nil
				}

				reservationID := ""
				if reservation != nil {
					reservationID = reservation.ID
				}

				applied = append(applied, appliedUpdate{
					LedgerKey:     ledgerKey,
					ReservationID: reservationID,
					OldUsage:      oldUsage.DeepCopy(),
					NewUsage:      newUsage.DeepCopy(),
				})
			}

			finalResp = nil

			return nil
		})
		if err != nil {
			if apierrors.IsConflict(err) {
				return ad.Denyf(
					"custom quota admission could not reserve usage due to concurrent quota updates after %d attempts; please retry the request: %v",
					customAdmissionBackoff.Steps,
					err,
				)
			}

			return ad.ErroredResponse(err)
		}

		return finalResp
	}
}

func (h *objectCalculationHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldObj, err := getUnstructured(req.OldObject)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		uid := oldObj.GetUID()
		if uid == "" {
			return nil
		}

		objRef := capsulev1beta2.QuantityLedgerObjectRef{
			APIGroup:   req.Kind.Group,
			APIVersion: req.Kind.Version,
			Kind:       req.Kind.Kind,
			Namespace:  oldObj.GetNamespace(),
			Name:       oldObj.GetName(),
			UID:        uid,
		}

		namespacedcq := &capsulev1beta2.CustomQuotaList{}
		if err := c.List(ctx, namespacedcq, client.InNamespace(req.Namespace), client.MatchingFields{
			index.ObjectUIDIndexerFieldName: string(uid),
		}); err != nil {
			return ad.ErroredResponse(err)
		}

		for _, nscq := range namespacedcq.Items {
			ledgerKey := types.NamespacedName{
				Name:      nscq.GetName(),
				Namespace: nscq.GetNamespace(),
			}

			if err := addLedgerPendingDelete(ctx, c, reader, ledgerKey, objRef); err != nil {
				return ad.ErroredResponse(err)
			}
		}

		globalcq := &capsulev1beta2.GlobalCustomQuotaList{}
		if err := c.List(ctx, globalcq, client.MatchingFields{
			index.ObjectUIDIndexerFieldName: string(uid),
		}); err != nil {
			return ad.ErroredResponse(err)
		}

		for _, gcq := range globalcq.Items {
			ledgerKey := types.NamespacedName{
				Name:      gcq.GetName(),
				Namespace: configuration.ControllerNamespace(),
			}

			if err := addLedgerPendingDelete(ctx, c, reader, ledgerKey, objRef); err != nil {
				return ad.ErroredResponse(err)
			}
		}

		return nil
	}
}

func deleteLedgerReservation(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	ledgerKey types.NamespacedName,
	reservationID string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, ledgerKey, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		active := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		released := resource.MustParse("0")

		for _, res := range ledger.Status.Reservations {
			if res.ID == reservationID {
				released.Add(res.Usage)

				continue
			}

			active = append(active, res)
		}

		if released.Sign() == 0 {
			return nil
		}

		allocated := ledger.Status.Allocated.DeepCopy()
		allocated.Sub(released)
		quota.ClampQuantityToZero(&allocated)

		reserved := resource.MustParse("0")
		for _, res := range active {
			reserved.Add(res.Usage)
		}

		ledger.Status.Reservations = active
		ledger.Status.Reserved = reserved
		ledger.Status.Allocated = allocated

		return c.Status().Update(ctx, ledger)
	})
}

func (h *objectCalculationHandler) matchAllQuotas(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	u unstructured.Unstructured,
) ([]quota.MatchedQuota, error) {
	namespaced, err := h.matchCustomQuotas(ctx, c, req, u)
	if err != nil {
		return nil, err
	}

	global, err := h.matchGlobalCustomQuotas(ctx, c, req, u)
	if err != nil {
		return nil, err
	}

	out := make([]quota.MatchedQuota, 0, len(namespaced)+len(global))

	out = append(out, namespaced...)

	out = append(out, global...)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Limit.Cmp(out[j].Limit) != 0 {
			return out[i].Limit.Cmp(out[j].Limit) < 0
		}

		if out[i].IsGlobal != out[j].IsGlobal {
			return out[i].IsGlobal
		}

		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}

		if out[i].SourceRank != out[j].SourceRank {
			return out[i].SourceRank < out[j].SourceRank
		}

		return out[i].Name < out[j].Name
	})

	return out, nil
}

func (h *objectCalculationHandler) matchCustomQuotas(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	u unstructured.Unstructured,
) ([]quota.MatchedQuota, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	list := &capsulev1beta2.CustomQuotaList{}

	err := c.List(ctx, list,
		client.InNamespace(req.Namespace),
		client.MatchingFields{
			index.TargetIndexerFieldName: req.Kind.String(),
		},
	)
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil
	}

	objLabels := labels.Set(u.GetLabels())
	out := make([]quota.MatchedQuota, 0)

	for _, cq := range list.Items {
		if !selectors.MatchesSelectors(objLabels, cq.Spec.ScopeSelectors) {
			continue
		}

		compiledTargets, err := h.getOrCompileCustomQuotaTargets(&cq)
		if err != nil {
			return nil, fmt.Errorf("compile targets for CustomQuota %s/%s: %w", cq.Namespace, cq.Name, err)
		}

		for i, target := range compiledTargets {
			if target.Group != req.Kind.Group ||
				target.Version != req.Kind.Version ||
				target.Kind != req.Kind.Kind {
				continue
			}

			matches, err := controller.MatchesCompiledSelectorsWithFields(u, target.CompiledSelectors)
			if err != nil {
				return nil, fmt.Errorf(
					"evaluate selectors for %s/%s on CustomQuota %s/%s: %w",
					u.GetNamespace(),
					u.GetName(),
					cq.Namespace,
					cq.Name,
					err,
				)
			}

			if !matches {
				continue
			}

			out = append(out, quota.MatchedQuota{
				Key:          controller.MakeCustomQuotaCacheKey(cq.Namespace, cq.Name),
				Name:         cq.Name,
				Namespace:    cq.Namespace,
				Path:         target.Path,
				CompiledPath: target.CompiledPath,
				Operation:    target.Operation,
				Limit:        cq.Spec.Limit.DeepCopy(),
				Used:         cq.Status.Usage.Used.DeepCopy(),
				IsGlobal:     false,
				SourceRank:   i,
			})
		}
	}

	return out, nil
}

func (h *objectCalculationHandler) matchGlobalCustomQuotas(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	u unstructured.Unstructured,
) ([]quota.MatchedQuota, error) {
	list := &capsulev1beta2.GlobalCustomQuotaList{}

	err := c.List(ctx, list, client.MatchingFields{
		index.TargetIndexerFieldName: req.Kind.String(),
	})
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, nil
	}

	objLabels := labels.Set(u.GetLabels())

	out := make([]quota.MatchedQuota, 0)

	for _, gcq := range list.Items {
		if !gcq.Status.NamespacePresent("*") && !gcq.Status.NamespacePresent(req.Namespace) {
			continue
		}

		if !selectors.MatchesSelectors(objLabels, gcq.Spec.ScopeSelectors) {
			continue
		}

		compiledTargets, err := h.getOrCompileGlobalCustomQuotaTargets(&gcq)
		if err != nil {
			return nil, fmt.Errorf("compile targets for GlobalCustomQuota %s: %w", gcq.Name, err)
		}

		for i, target := range compiledTargets {
			if target.Group != req.Kind.Group ||
				target.Version != req.Kind.Version ||
				target.Kind != req.Kind.Kind {
				continue
			}

			matches, err := controller.MatchesCompiledSelectorsWithFields(u, target.CompiledSelectors)
			if err != nil {
				return nil, fmt.Errorf(
					"evaluate selectors for %s/%s on GlobalCustomQuota %s: %w",
					u.GetNamespace(),
					u.GetName(),
					gcq.Name,
					err,
				)
			}

			if !matches {
				continue
			}

			out = append(out, quota.MatchedQuota{
				Key:          controller.MakeGlobalCustomQuotaCacheKey(gcq.Name),
				Name:         gcq.Name,
				Namespace:    "",
				Path:         target.Path,
				CompiledPath: target.CompiledPath,
				Operation:    target.Operation,
				Limit:        gcq.Spec.Limit.DeepCopy(),
				Used:         gcq.Status.Usage.Used.DeepCopy(),
				IsGlobal:     true,
				SourceRank:   i,
			})
		}
	}

	return out, nil
}

func getUnstructured(rawExt runtime.RawExtension) (unstructured.Unstructured, error) {
	var (
		obj   runtime.Object
		scope conversion.Scope
	)

	err := runtime.Convert_runtime_RawExtension_To_runtime_Object(&rawExt, &obj, scope)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	innerObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	u := unstructured.Unstructured{Object: innerObj}

	return u, nil
}

func quotaTypeName(global bool) string {
	if global {
		return "GlobalCustomQuota"
	}

	return "CustomQuota"
}

type evaluatedQuota struct {
	quota.MatchedQuota

	Usage resource.Quantity
}

func (h *objectCalculationHandler) evaluateMatchedQuotas(
	ctx context.Context,
	u unstructured.Unstructured,
	matched []quota.MatchedQuota,
) ([]evaluatedQuota, error) {
	log := log.FromContext(ctx)

	usageByPath := make(map[string]resource.Quantity, len(matched))

	for _, mq := range matched {
		// count does not use a path
		if mq.Operation == quota.OpCount {
			continue
		}

		if _, ok := usageByPath[mq.Path]; ok {
			continue
		}

		usage, err := quota.ParseQuantityFromUnstructured(u, mq.CompiledPath)
		if err != nil {
			return nil, fmt.Errorf(
				"%s %q source path %q op %q did not resolve to a valid quantity: %w",
				quotaTypeName(mq.IsGlobal),
				mq.Name,
				mq.Path,
				mq.Operation,
				err,
			)
		}

		log.V(5).Info("parsed usage", "path", mq.Path, "parsed", usage.String())

		usageByPath[mq.Path] = usage
	}

	byKey := make(map[string]evaluatedQuota, len(matched))

	order := make([]string, 0, len(matched))

	for _, mq := range matched {
		ev, ok := byKey[mq.Key]
		if !ok {
			ev = evaluatedQuota{
				MatchedQuota: mq,
				Usage:        resource.MustParse("0"),
			}

			order = append(order, mq.Key)
		}

		var usage resource.Quantity

		switch mq.Operation {
		case quota.OpCount:
			usage = *resource.NewQuantity(1, resource.DecimalSI)

		case quota.OpSub:
			usage = usageByPath[mq.Path].DeepCopy()
			usage.Neg()
			ev.Usage.Add(usage)
			quota.ClampQuantityToZero(&ev.Usage)
			byKey[mq.Key] = ev

			continue

		case quota.OpAdd:
			usage = usageByPath[mq.Path].DeepCopy()

		default:
			return nil, fmt.Errorf("unsupported quota operation %q for key %q", mq.Operation, mq.Key)
		}

		ev.Usage.Add(usage)
		byKey[mq.Key] = ev
	}

	out := make([]evaluatedQuota, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}

	return out, nil
}

func addLedgerPendingDelete(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	ledgerKey types.NamespacedName,
	objRef capsulev1beta2.QuantityLedgerObjectRef,
) error {
	return retry.RetryOnConflict(ledgerMutationBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := reader.Get(ctx, ledgerKey, ledger); err != nil {
			return err
		}

		now := metav1.Now()

		for _, pd := range ledger.Status.PendingDeletes {
			if pd.ObjectRef.UID != "" && pd.ObjectRef.UID == objRef.UID {
				return nil
			}
		}

		ledger.Status.PendingDeletes = append(ledger.Status.PendingDeletes, capsulev1beta2.QuantityLedgerPendingDelete{
			ObjectRef: objRef,
			CreatedAt: now,
		})

		return c.Status().Update(ctx, ledger)
	})
}

func (h *objectCalculationHandler) getOrCompileCustomQuotaTargets(
	cq *capsulev1beta2.CustomQuota,
) ([]cache.CompiledTarget, error) {
	key := controller.MakeCustomQuotaCacheKey(cq.Namespace, cq.Name)

	return h.targetsCache.GetOrBuild(key, func() ([]cache.CompiledTarget, error) {
		targets := make([]capsulev1beta2.CustomQuotaStatusTarget, 0, len(cq.Spec.Sources))
		for _, src := range cq.Spec.Sources {
			targets = append(targets, capsulev1beta2.CustomQuotaStatusTarget{
				GroupVersionKind:            metav1.GroupVersionKind(src.GroupVersionKind()),
				CustomQuotaSpecSourceConfig: src.CustomQuotaSpecSourceConfig,
			})
		}

		return controller.CompileTargets(h.jsonPathCache, targets)
	})
}

func (h *objectCalculationHandler) getOrCompileGlobalCustomQuotaTargets(
	gcq *capsulev1beta2.GlobalCustomQuota,
) ([]cache.CompiledTarget, error) {
	key := controller.MakeGlobalCustomQuotaCacheKey(gcq.Name)

	return h.targetsCache.GetOrBuild(key, func() ([]cache.CompiledTarget, error) {
		targets := make([]capsulev1beta2.CustomQuotaStatusTarget, 0, len(gcq.Spec.Sources))
		for _, src := range gcq.Spec.Sources {
			targets = append(targets, capsulev1beta2.CustomQuotaStatusTarget{
				GroupVersionKind:            metav1.GroupVersionKind(src.GroupVersionKind()),
				CustomQuotaSpecSourceConfig: src.CustomQuotaSpecSourceConfig,
			})
		}

		return controller.CompileTargets(h.jsonPathCache, targets)
	})
}

func evaluatedByKey(in []evaluatedQuota) map[string]evaluatedQuota {
	out := make(map[string]evaluatedQuota, len(in))
	for _, item := range in {
		existing, ok := out[item.Key]
		if !ok {
			copyItem := item
			copyItem.Usage = item.Usage.DeepCopy()
			out[item.Key] = copyItem

			continue
		}

		existing.Usage.Add(item.Usage)
		quota.ClampQuantityToZero(&existing.Usage)
		out[item.Key] = existing
	}

	return out
}
