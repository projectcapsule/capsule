// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"context"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func (h *objectCalculationHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
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

		matched, err := h.matchAllQuotas(ctx, c, req, u)
		if err != nil {
			return ad.ErroredResponse(err)
		}
		if len(matched) == 0 {
			return nil
		}

		evaluated, err := h.evaluateMatchedQuotas(ctx, u, matched)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		type appliedReservation struct {
			LedgerKey     types.NamespacedName
			ReservationID string
		}

		applied := make([]appliedReservation, 0, len(evaluated))

		for _, item := range evaluated {
			ledgerKey := quantityLedgerKeyForMatchedQuota(item)
			reservation := buildReservation(req, u, item.Usage)

			allowed, effectiveUsed, reserved, err := upsertLedgerReservation(
				ctx,
				c,
				ledgerKey,
				item.Used,
				item.Limit,
				reservation,
			)

			if err != nil {
				for _, a := range applied {
					_ = deleteLedgerReservation(ctx, c, a.LedgerKey, a.ReservationID)
				}
				return ad.ErroredResponse(err)
			}

			if !allowed {
				for _, a := range applied {
					_ = deleteLedgerReservation(ctx, c, a.LedgerKey, a.ReservationID)
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

				resp := admission.Denied(
					fmt.Sprintf(
						"creating resource exceeds limit for %s %q (requested=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
						quotaTypeName(item.IsGlobal),
						item.Name,
						item.Usage.String(),
						effectiveUsed.String(),
						available.String(),
						item.Limit.String(),
						reserved.String(),
					),
				)

				return &resp
			}

			applied = append(applied, appliedReservation{
				LedgerKey:     ledgerKey,
				ReservationID: reservation.ID,
			})
		}

		return nil
	}
}

func (h *objectCalculationHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldObj, err := getUnstructured(req.OldObject)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newObj, err := getUnstructured(req.Object)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		oldMatched, err := h.matchAllQuotas(ctx, c, req, oldObj)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newMatched, err := h.matchAllQuotas(ctx, c, req, newObj)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		oldEvaluated, err := h.evaluateMatchedQuotas(ctx, oldObj, oldMatched)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newEvaluated, err := h.evaluateMatchedQuotas(ctx, newObj, newMatched)
		if err != nil {
			return ad.ErroredResponse(err)
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
			return nil
		}

		type appliedReservation struct {
			LedgerKey     types.NamespacedName
			ReservationID string
		}

		applied := make([]appliedReservation, 0, len(oldByKey)+len(newByKey))

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

			targetUsage := resource.MustParse("0")
			if hadNew {
				targetUsage = newItem.Usage.DeepCopy()
			}

			ledgerKey := quantityLedgerKeyForMatchedQuota(base)

			objRef := capsulev1beta2.QuantityLedgerObjectRef{
				APIGroup:   req.Kind.Group,
				APIVersion: req.Kind.Version,
				Kind:       req.Kind.Kind,
				Namespace:  oldObj.GetNamespace(),
				Name:       oldObj.GetName(),
				UID:        oldObj.GetUID(),
			}

			// Old object matched this quota, new object no longer matches:
			// keep reconciling until the old materialized claim is gone.
			if hadOld && !hadNew {
				if err := addLedgerPendingDelete(ctx, c, ledgerKey, objRef); err != nil {
					for _, a := range applied {
						_ = deleteLedgerReservation(ctx, c, a.LedgerKey, a.ReservationID)
					}

					return ad.ErroredResponse(err)
				}
			}

			reservation := buildReservation(req, newObj, targetUsage)

			allowed, effectiveUsed, reserved, err := upsertLedgerReservation(
				ctx,
				c,
				ledgerKey,
				base.Used,
				base.Limit,
				reservation,
			)
			if err != nil {
				for _, a := range applied {
					_ = deleteLedgerReservation(ctx, c, a.LedgerKey, a.ReservationID)
				}

				return ad.ErroredResponse(err)
			}
			if !allowed {
				for _, a := range applied {
					_ = deleteLedgerReservation(ctx, c, a.LedgerKey, a.ReservationID)
				}

				available := base.Limit.DeepCopy()

				available.Sub(effectiveUsed)

				if available.Sign() < 0 {
					available = resource.MustParse("0")
				}

				resp := admission.Denied(
					fmt.Sprintf(
						"updating resource exceeds limit for %s %q (requested=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
						quotaTypeName(base.IsGlobal),
						base.Name,
						targetUsage.String(),
						effectiveUsed.String(),
						available.String(),
						base.Limit.String(),
						reserved.String(),
					),
				)

				return &resp
			}

			applied = append(applied, appliedReservation{
				LedgerKey:     ledgerKey,
				ReservationID: reservation.ID,
			})
		}

		return nil
	}
}

func (h *objectCalculationHandler) OnDelete(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
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

			if err := addLedgerPendingDelete(ctx, c, ledgerKey, objRef); err != nil {
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

			if err := addLedgerPendingDelete(ctx, c, ledgerKey, objRef); err != nil {
				return ad.ErroredResponse(err)
			}
		}

		return nil
	}
}

func deleteLedgerReservation(
	ctx context.Context,
	c client.Client,
	ledgerKey types.NamespacedName,
	reservationID string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := c.Get(ctx, ledgerKey, ledger); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		active := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		for _, res := range ledger.Status.Reservations {
			if res.ID == reservationID {
				continue
			}

			active = append(active, res)
		}

		ledger.Status.Reservations = active

		ledger.Status.Reserved = resource.MustParse("0")

		for _, res := range active {
			ledger.Status.Reserved.Add(res.Usage)
		}

		return c.Status().Update(ctx, ledger)
	})
}

func (h *objectCalculationHandler) effectiveAvailableForSort(
	ctx context.Context,
	c client.Client,
	mq quota.MatchedQuota,
) resource.Quantity {
	used := mq.Used.DeepCopy()

	var ledgerKey types.NamespacedName
	if mq.IsGlobal {
		ledgerKey = types.NamespacedName{
			Name:      mq.Name,
			Namespace: configuration.ControllerNamespace(),
		}
	} else {
		ledgerKey = types.NamespacedName{
			Name:      mq.Name,
			Namespace: mq.Namespace,
		}
	}

	ledger := &capsulev1beta2.QuantityLedger{}
	if err := c.Get(ctx, ledgerKey, ledger); err == nil {
		used.Add(ledger.Status.Reserved)

		quota.ClampQuantityToZero(&used)
	}

	available := mq.Limit.DeepCopy()

	available.Sub(used)

	if available.Sign() < 0 {
		return resource.MustParse("0")
	}

	return available
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
		availI := h.effectiveAvailableForSort(ctx, c, out[i])
		availJ := h.effectiveAvailableForSort(ctx, c, out[j])

		cmp := availI.Cmp(availJ)
		if cmp != 0 {
			return cmp < 0
		}

		cmp = out[i].Limit.Cmp(out[j].Limit)
		if cmp != 0 {
			return cmp < 0
		}

		if out[i].IsGlobal != out[j].IsGlobal {
			return out[i].IsGlobal
		}

		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
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
			if target.GroupVersionKind.Group != req.Kind.Group ||
				target.GroupVersionKind.Version != req.Kind.Version ||
				target.GroupVersionKind.Kind != req.Kind.Kind {
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
				Operation:    quota.Operation(target.Operation),
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
			if target.GroupVersionKind.Group != req.Kind.Group ||
				target.GroupVersionKind.Version != req.Kind.Version ||
				target.GroupVersionKind.Kind != req.Kind.Kind {
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
				Operation:    quota.Operation(target.Operation),
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
			return nil, fmt.Errorf("parse quantity from path %q: %w", mq.Path, err)
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
func upsertLedgerReservation(
	ctx context.Context,
	c client.Client,
	ledgerKey types.NamespacedName,
	persistedUsed resource.Quantity,
	limit resource.Quantity,
	reservation capsulev1beta2.QuantityLedgerReservation,
) (bool, resource.Quantity, resource.Quantity, error) {
	var allowed bool

	var effectiveUsed resource.Quantity

	var reserved resource.Quantity

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := c.Get(ctx, ledgerKey, ledger); err != nil {
			return err
		}

		now := metav1.Now()

		active := make([]capsulev1beta2.QuantityLedgerReservation, 0, len(ledger.Status.Reservations))
		found := false

		for _, existing := range ledger.Status.Reservations {
			if existing.ExpiresAt != nil && existing.ExpiresAt.Before(&now) {
				continue
			}

			if existing.ID == reservation.ID {
				existing.Usage = reservation.Usage.DeepCopy()
				existing.ObjectRef = reservation.ObjectRef
				existing.UpdatedAt = now
				existing.ExpiresAt = reservation.ExpiresAt
				found = true
			}

			active = append(active, existing)
		}

		if !found {
			active = append(active, reservation)
		}

		newReserved := resource.MustParse("0")

		for _, r := range active {
			newReserved.Add(r.Usage)
		}

		newEffectiveUsed := persistedUsed.DeepCopy()

		newEffectiveUsed.Add(newReserved)

		if newEffectiveUsed.Sign() < 0 {
			newEffectiveUsed = resource.MustParse("0")
		}

		if newEffectiveUsed.Cmp(limit) > 0 {
			allowed = false
			effectiveUsed = newEffectiveUsed
			reserved = newReserved
			return nil
		}

		ledger.Status.Reservations = active

		ledger.Status.Reserved = newReserved

		if err := c.Status().Update(ctx, ledger); err != nil {
			return err
		}

		allowed = true

		effectiveUsed = newEffectiveUsed

		reserved = newReserved

		return nil
	})

	return allowed, effectiveUsed, reserved, err
}

func addLedgerPendingDelete(
	ctx context.Context,
	c client.Client,
	ledgerKey types.NamespacedName,
	objRef capsulev1beta2.QuantityLedgerObjectRef,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ledger := &capsulev1beta2.QuantityLedger{}
		if err := c.Get(ctx, ledgerKey, ledger); err != nil {
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
				CustomQuotaSpecSource: src,
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
				CustomQuotaSpecSource: src,
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
