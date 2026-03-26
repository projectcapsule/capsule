// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"context"
	"fmt"
	"sort"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	controller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	index "github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

type objectCalculationHandler struct {
	cache *cache.QuantityCache[string]

	globalNotifier    chan<- event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota]
	namespaceNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.CustomQuota]
}

func ObjectCalculationHandler(
	cache *cache.QuantityCache[string],
	namespaceNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.CustomQuota],
	globalNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota],
) handlers.Handler {
	return &objectCalculationHandler{
		cache:             cache,
		globalNotifier:    globalNotifier,
		namespaceNotifier: namespaceNotifier,
	}
}

func (h *objectCalculationHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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

		reserved, denied := h.reserveEvaluatedQuotas(evaluated)
		if denied != nil {
			return denied
		}

		for _, item := range reserved {
			if item.IsGlobal {
				h.enqueueGlobalCustomQuota(item.Name)
			} else {
				h.enqueueCustomQuota(item.Namespace, item.Name)
			}
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

		// Fast path: if labels did not change and all tracked quantities stayed the same,
		// just notify controllers and return.
		relevantChange := labelsChanged(oldObj, newObj) || len(oldByKey) != len(newByKey)
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
			if err := h.notifyControllers(ctx, c, oldObj, req); err != nil {
				return ad.ErroredResponse(err)
			}
			return nil
		}

		type reservation struct {
			Key   string
			Usage resource.Quantity
		}
		var reserved []reservation

		// Validate positive deltas and reserve them.
		for key, newItem := range newByKey {
			oldItem, existedBefore := oldByKey[key]

			delta := newItem.Usage.DeepCopy()
			if existedBefore {
				delta.Sub(oldItem.Usage)
			}

			switch delta.Sign() {
			case 1:
				allowed, effectiveUsed, entry := h.cache.CheckAndReserve(
					newItem.Key,
					newItem.Used,
					newItem.Limit,
					delta,
				)
				if !allowed {
					for _, r := range reserved {
						h.cache.Release(r.Key, r.Usage)
					}

					available := newItem.Limit.DeepCopy()
					available.Sub(effectiveUsed)
					if available.Sign() < 0 {
						available = resource.MustParse("0")
					}

					resp := admission.Denied(
						fmt.Sprintf(
							"updating resource exceeds limit for %s %q (requestedDelta=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
							quotaTypeName(newItem.IsGlobal),
							newItem.Name,
							delta.String(),
							effectiveUsed.String(),
							available.String(),
							newItem.Limit.String(),
							entry.Reserved.String(),
						),
					)
					return &resp
				}

				reserved = append(reserved, reservation{
					Key:   newItem.Key,
					Usage: delta.DeepCopy(),
				})
			case -1:
				release := delta.DeepCopy()
				release.Neg()
				h.cache.Release(newItem.Key, release)
			}
		}

		// Quotas that matched before but not anymore: notify controllers only.
		// The controller rebuild will drop them from claims/usage.
		if err := h.notifyControllers(ctx, c, oldObj, req); err != nil {
			for _, r := range reserved {
				h.cache.Release(r.Key, r.Usage)
			}

			return ad.ErroredResponse(err)
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

		namespacedcq := &capsulev1beta2.CustomQuotaList{}
		err = c.List(ctx, namespacedcq, client.InNamespace(req.Namespace), client.MatchingFields{
			index.ObjectUIDIndexerFieldName: string(uid),
		})
		if err != nil {
			return ad.ErroredResponse(err)
		}
		for _, nscq := range namespacedcq.Items {
			key := controller.MakeCustomQuotaCacheKey(nscq.GetNamespace(), nscq.GetName())
			h.cache.AddPendingDelete(key, uid)
			h.enqueueCustomQuota(nscq.GetNamespace(), nscq.GetName())
		}

		globalcq := &capsulev1beta2.GlobalCustomQuotaList{}
		err = c.List(ctx, globalcq, client.MatchingFields{
			index.ObjectUIDIndexerFieldName: string(uid),
		})
		if err != nil {
			return ad.ErroredResponse(err)
		}

		for _, gcq := range globalcq.Items {
			key := controller.MakeGlobalCustomQuotaCacheKey(gcq.GetName())
			h.cache.AddPendingDelete(key, uid)
			h.enqueueGlobalCustomQuota(gcq.GetName())
		}

		return nil
	}
}

func (h *objectCalculationHandler) effectiveAvailableForSort(mq quota.MatchedQuota) resource.Quantity {
	used := mq.Used.DeepCopy()

	if h.cache != nil {
		if entry, ok := h.cache.Get(mq.Key); ok {
			used.Add(entry.Reserved)
		}
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
		availI := h.effectiveAvailableForSort(out[i])
		availJ := h.effectiveAvailableForSort(out[j])

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
	out := make([]quota.MatchedQuota, 0, len(list.Items))

	for _, cq := range list.Items {
		if !selectors.MatchesSelectors(objLabels, cq.Spec.ScopeSelectors) {
			continue
		}

		out = append(out, quota.MatchedQuota{
			Key:       controller.MakeCustomQuotaCacheKey(cq.Namespace, cq.Name),
			Name:      cq.Name,
			Namespace: cq.Namespace,
			Path:      cq.Spec.Source.Path,
			Limit:     cq.Spec.Limit.DeepCopy(),
			Used:      cq.Status.Usage.Used.DeepCopy(),
			IsGlobal:  false,
		})
	}

	return out, nil
}

func (h *objectCalculationHandler) matchGlobalCustomQuotas(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	u unstructured.Unstructured,
) ([]quota.MatchedQuota, error) {
	log := log.FromContext(ctx)

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

	log.Info("FOUND GLOBAL", "SIZE", len(list.Items))

	objLabels := labels.Set(u.GetLabels())
	out := make([]quota.MatchedQuota, 0, len(list.Items))

	for _, gcq := range list.Items {
		if gcq.Status.Target.Scope == k8smeta.RESTScopeNamespace.Name() {
			if !gcq.Status.NamespacePresent("*") && !gcq.Status.NamespacePresent(req.Namespace) {
				continue
			}
		}

		if !selectors.MatchesSelectors(objLabels, gcq.Spec.ScopeSelectors) {
			continue
		}

		out = append(out, quota.MatchedQuota{
			Key:       controller.MakeGlobalCustomQuotaCacheKey(gcq.Name),
			Name:      gcq.Name,
			Namespace: "",
			Path:      gcq.Spec.Source.Path,
			Limit:     gcq.Spec.Limit.DeepCopy(),
			Used:      gcq.Status.Usage.Used.DeepCopy(),
			IsGlobal:  true,
		})
	}

	return out, nil
}

// Triggers a reconcile based on UID apperance
func (h *objectCalculationHandler) notifyControllers(
	ctx context.Context,
	c client.Client,
	obj unstructured.Unstructured,
	req admission.Request,
) error {
	uid := string(obj.GetUID())
	if uid == "" {
		return nil
	}

	namespacedcq := &capsulev1beta2.CustomQuotaList{}
	err := c.List(ctx, namespacedcq, client.InNamespace(req.Namespace), client.MatchingFields{
		index.ObjectUIDIndexerFieldName: uid,
	})
	if err != nil {
		return err
	}

	for _, nscq := range namespacedcq.Items {
		h.enqueueCustomQuota(nscq.GetNamespace(), nscq.GetName())
	}

	globalcq := &capsulev1beta2.GlobalCustomQuotaList{}
	err = c.List(ctx, globalcq, client.MatchingFields{
		index.ObjectUIDIndexerFieldName: uid,
	})
	if err != nil {
		return err
	}

	for _, nscq := range globalcq.Items {
		h.enqueueGlobalCustomQuota(nscq.GetName())
	}

	return nil
}

// Trigger the Controller
func (h *objectCalculationHandler) enqueueCustomQuota(namespace, name string) {
	obj := &capsulev1beta2.CustomQuota{}
	obj.SetName(name)
	obj.SetNamespace(namespace)

	ev := event.TypedGenericEvent[*capsulev1beta2.CustomQuota]{
		Object: obj,
	}

	select {
	case h.namespaceNotifier <- ev:
	default:
		// best effort: never block admission
	}
}

// Trigger the Controller
func (h *objectCalculationHandler) enqueueGlobalCustomQuota(name string) {
	if h.globalNotifier == nil {
		return
	}

	obj := &capsulev1beta2.GlobalCustomQuota{}
	obj.SetName(name)

	ev := event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota]{
		Object: obj,
	}

	select {
	case h.globalNotifier <- ev:
	default:
		// best effort: never block admission
	}
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

	groupedByPath := make(map[string][]quota.MatchedQuota, len(matched))
	for _, mq := range matched {
		groupedByPath[mq.Path] = append(groupedByPath[mq.Path], mq)
	}

	usageByPath := make(map[string]resource.Quantity, len(groupedByPath))
	for path := range groupedByPath {
		usage, err := quota.ParseQuantityFromUnstructured(u, path)
		if err != nil {
			return nil, err
		}

		log.V(5).Info("parsed usage", "parsed", usage)

		usageByPath[path] = usage
	}

	evaluated := make([]evaluatedQuota, 0, len(matched))
	for _, mq := range matched {
		evaluated = append(evaluated, evaluatedQuota{
			MatchedQuota: mq,
			Usage:        usageByPath[mq.Path].DeepCopy(),
		})
	}

	return evaluated, nil
}

func (h *objectCalculationHandler) reserveEvaluatedQuotas(
	evaluated []evaluatedQuota,
) (reserved []evaluatedQuota, denied *admission.Response) {
	reserved = make([]evaluatedQuota, 0, len(evaluated))

	for _, item := range evaluated {
		allowed, effectiveUsed, entry := h.cache.CheckAndReserve(
			item.Key,
			item.Used,
			item.Limit,
			item.Usage,
		)
		if !allowed {
			for _, reservedItem := range reserved {
				h.cache.Release(reservedItem.Key, reservedItem.Usage)
			}

			available := item.Limit.DeepCopy()
			available.Sub(effectiveUsed)
			if available.Sign() < 0 {
				available = resource.MustParse("0")
			}

			resp := admission.Denied(
				fmt.Sprintf(
					"updating resource exceeds limit for %s %q (requested=%s, currentUsed=%s, available=%s, limit=%s, inflightReserved=%s)",
					quotaTypeName(item.IsGlobal),
					item.Name,
					item.Usage.String(),
					effectiveUsed.String(),
					available.String(),
					item.Limit.String(),
					entry.Reserved.String(),
				),
			)
			return reserved, &resp
		}

		reserved = append(reserved, item)
	}

	return reserved, nil
}

func evaluatedByKey(in []evaluatedQuota) map[string]evaluatedQuota {
	out := make(map[string]evaluatedQuota, len(in))
	for _, item := range in {
		out[item.Key] = item
	}
	return out
}

func labelsChanged(oldObj, newObj unstructured.Unstructured) bool {
	return !labels.Equals(labels.Set(oldObj.GetLabels()), labels.Set(newObj.GetLabels()))
}
