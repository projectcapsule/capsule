// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	index "github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
)

type customquotasHandler struct {
	cache *cache.QuantityCache[string]

	globalNotifier    chan<- event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota]
	namespaceNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.CustomQuota]
}

func CustomQuotasHandler(
	cache *cache.QuantityCache[string],
	namespaceNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.CustomQuota],
	globalNotifier chan<- event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota],
) handlers.Handler {
	return &customquotasHandler{
		cache:             cache,
		globalNotifier:    globalNotifier,
		namespaceNotifier: namespaceNotifier,
	}
}

func (h *customquotasHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		log := log.FromContext(ctx)

		u, err := getUnstructured(req.Object)
		if err != nil {
			log.Error(err, "error getting unstructured")
			return nil
		}

		matched, err := h.MatchingCustomQuota(ctx, c, req, u)
		if err != nil {
			log.Error(err, "error matching custom quotas")
			return nil
		}

		if len(matched) == 0 {
			return nil
		}

		groupedByPath := make(map[string][]capsulev1beta2.CustomQuota, len(matched))
		for _, cq := range matched {
			groupedByPath[cq.Spec.Source.Path] = append(groupedByPath[cq.Spec.Source.Path], cq)
		}

		usageByPath := make(map[string]resource.Quantity, len(groupedByPath))
		for path, quotas := range groupedByPath {
			usageValue, err := clt.GetUsageFromUnstructured(u, path)
			if err != nil {
				for _, cq := range quotas {
					log.Error(err, "error getting usage from object", "customQuota", cq.Name, "path", path)
				}
				usageByPath[path] = resource.MustParse("0")
				continue
			}

			usage, err := resource.ParseQuantity(usageValue)
			if err != nil {
				for _, cq := range quotas {
					log.Error(err, "error parsing usage quantity", "customQuota", cq.Name, "path", path, "value", usageValue)
				}
				usageByPath[path] = resource.MustParse("0")
				continue
			}

			usageByPath[path] = usage
		}

		type evaluatedQuota struct {
			quota capsulev1beta2.CustomQuota
			key   string
			path  string
			usage resource.Quantity
		}

		evaluated := make([]evaluatedQuota, 0, len(matched))
		for _, cq := range matched {
			path := cq.Spec.Source.Path
			evaluated = append(evaluated, evaluatedQuota{
				quota: cq,
				key:   makeCustomQuotaCacheKey(cq),
				path:  path,
				usage: usageByPath[path].DeepCopy(),
			})
		}

		// Fail fast on the smallest limit first.
		sort.Slice(evaluated, func(i, j int) bool {
			return evaluated[i].quota.Spec.Limit.Cmp(evaluated[j].quota.Spec.Limit) < 0
		})

		reserved := make([]evaluatedQuota, 0, len(evaluated))

		for _, item := range evaluated {
			allowed, avail, limit := h.cache.CheckAndReserve(
				item.key,
				item.quota.Status.Usage.Used,
				item.quota.Spec.Limit,
				item.usage,
			)
			if !allowed {
				for _, reservedItem := range reserved {
					h.cache.Release(reservedItem.key, reservedItem.usage)
				}

				response := admission.Denied(
					fmt.Sprintf("creating resource exceeds limit for CustomQuota %s (requested=%s, available=%s)", item.quota.Name, avail.String(), &limit.Reserved),
				)
				return &response
			}

			reserved = append(reserved, item)
		}

		for _, item := range reserved {
			h.enqueueCustomQuota(item.quota.Namespace, item.quota.Name)
		}

		return nil
	}
}

func (h *customquotasHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldObj, err := getUnstructured(req.OldObject)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		err = h.notifyControllers(ctx, c, oldObj, req)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		return nil
	}
}

func (h *customquotasHandler) OnDelete(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
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
			key := makeCustomQuotaCacheKey(nscq)
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

		for _, nscq := range globalcq.Items {
			h.enqueueGlobalCustomQuota(nscq.GetName())
		}

		return nil
	}
}

func (h *customquotasHandler) MatchingCustomQuota(ctx context.Context, c client.Client, req admission.Request, u unstructured.Unstructured) ([]capsulev1beta2.CustomQuota, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	list := &capsulev1beta2.CustomQuotaList{}
	err := c.List(ctx, list, client.InNamespace(req.Namespace), client.MatchingFields{
		index.TargetIndexerFieldName: req.Kind.String(),
	})
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, err
	}

	objLabels := labels.Set(u.GetLabels())
	customQuotasMatched := make([]capsulev1beta2.CustomQuota, 0, len(list.Items))

	for _, cq := range list.Items {
		for _, selector := range cq.Spec.ScopeSelectors {
			sel, err := metav1.LabelSelectorAsSelector(&selector)
			if err != nil {
				continue
			}

			if sel.Matches(objLabels) {
				customQuotasMatched = append(customQuotasMatched, cq)
				break
			}
		}
	}

	sort.Slice(customQuotasMatched, func(i, j int) bool {
		return customQuotasMatched[i].Spec.Limit.Cmp(customQuotasMatched[j].Spec.Limit) < 0
	})

	return customQuotasMatched, nil
}

// Triggers a reconcile based on UID apperance
func (h *customquotasHandler) notifyControllers(
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

func makeCustomQuotaCacheKey(cq capsulev1beta2.CustomQuota) string {
	return cq.Namespace + "/" + cq.Name
}

func groupCustomQuotasByPath(quotas []capsulev1beta2.CustomQuota) map[string][]capsulev1beta2.CustomQuota {
	grouped := make(map[string][]capsulev1beta2.CustomQuota, len(quotas))
	for _, cq := range quotas {
		path := cq.Spec.Source.Path
		grouped[path] = append(grouped[path], cq)
	}

	return grouped
}

func evaluateUsageByPath(
	u unstructured.Unstructured,
	grouped map[string][]capsulev1beta2.CustomQuota,
) map[string]resource.Quantity {
	usages := make(map[string]resource.Quantity, len(grouped))

	for path := range grouped {
		usageValue, err := clt.GetUsageFromUnstructured(u, path)
		if err != nil {
			usages[path] = resource.MustParse("0")
			continue
		}

		usage, err := resource.ParseQuantity(usageValue)
		if err != nil {
			usages[path] = resource.MustParse("0")
			continue
		}

		usages[path] = usage
	}

	return usages
}

// Trigger the Controller
func (h *customquotasHandler) enqueueCustomQuota(namespace, name string) {
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
func (h *customquotasHandler) enqueueGlobalCustomQuota(name string) {
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
