// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/customquotas"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type customquotasHandler struct {
	client client.Client
	log    logr.Logger
}

func CustomQuotasHandler(client client.Client, log logr.Logger) capsulewebhook.Handler {
	return &customquotasHandler{
		client: client,
		log:    log,
	}
}

func (h *customquotasHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		u, err := getUnstructured(req.Object)
		if err != nil {
			h.log.Error(err, "error getting unstructured")

			return nil
		}

		customQuotasMatched, errNamespaced := h.getCustomQuotaMatched(ctx, req, u)
		clusterCustomQuotasMatched, errCluster := h.getClusterCustomQuotaMatched(ctx, req, u)

		err = errors.Join(errNamespaced, errCluster)
		if err != nil {
			h.log.Error(err, "error getting matched CustomQuotas")

			return nil
		}

		customQuotasMatched = append(customQuotasMatched, clusterCustomQuotasMatched...)

		for _, cq := range customQuotasMatched {
			typeName := getType(cq)

			usageValue, err := customquotas.GetUsageFromUnstructured(u, cq.Spec.Source.Path)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error getting usage from object for %s %s: %v", typeName, cq.Name, err))

				continue
			}

			usage, err := resource.ParseQuantity(usageValue)
			if err != nil {
				usage = resource.MustParse("0")
			}

			newUsed := cq.Status.Used.DeepCopy()
			newUsed.Add(usage)

			if newUsed.Cmp(cq.Spec.Limit) == 1 {
				response := admission.Denied(fmt.Sprintf("updating resource exceeds limit for %s %s", typeName, cq.Name))

				return &response
			}

			cq.Status.Used.Add(usage)
			cq.Status.Available = cq.Spec.Limit.DeepCopy()
			cq.Status.Available.Sub(cq.Status.Used)
			cq.Status.Claims = append(cq.Status.Claims, fmt.Sprintf("%s.%s", req.Namespace, req.Name))

			err = h.updateSubResStatusCustomQuota(ctx, cq)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error updating Sub-Resource for %s %s status", typeName, cq.Name))
			}
		}

		return nil
	}
}

func (h *customquotasHandler) OnDelete(c client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		obj, err := getUnstructured(req.OldObject)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error getting unstructured: %v", err))

			return nil
		}

		customQuotasMatched, errNamespaced := h.getCustomQuotaMatched(ctx, req, obj)
		clusterCustomQuotasMatched, errCluster := h.getClusterCustomQuotaMatched(ctx, req, obj)

		err = errors.Join(errNamespaced, errCluster)
		if err != nil {
			h.log.Error(err, "error getting matched CustomQuotas")

			return nil
		}

		customQuotasMatched = append(customQuotasMatched, clusterCustomQuotasMatched...)

		claim := fmt.Sprintf("%s.%s", req.Namespace, req.Name)

		for _, cq := range customQuotasMatched {
			typeName := getType(cq)

			claimList := cq.Status.Claims
			if !slices.Contains(claimList, claim) {
				continue
			}

			err = h.deleteResourceFromCustomQuota(ctx, obj, cq)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error deleting resource from %s %s", typeName, cq.Name))
			}
		}

		return nil
	}
}

func (h *customquotasHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldObj, errOldUnstructured := getUnstructured(req.OldObject)
		newObj, errNewUnstructured := getUnstructured(req.Object)
		oldCustomQuotasMatched, errOldMatch := h.getCustomQuotaMatched(ctx, req, oldObj)
		oldClusterCustomQuotasMatched, errClusterOldMatch := h.getClusterCustomQuotaMatched(ctx, req, oldObj)
		newCustomQuotasMatched, errNewMatch := h.getCustomQuotaMatched(ctx, req, newObj)
		newClusterCustomQuotasMatched, errClusterNewMatch := h.getClusterCustomQuotaMatched(ctx, req, newObj)

		err := errors.Join(errOldUnstructured, errNewUnstructured, errOldMatch, errClusterOldMatch, errNewMatch, errClusterNewMatch)
		if err != nil {
			h.log.Error(err, "error getting old and new unstructured or matched CustomQuotas")

			return nil
		}

		oldCustomQuotasMatched = append(oldCustomQuotasMatched, oldClusterCustomQuotasMatched...)
		newCustomQuotasMatched = append(newCustomQuotasMatched, newClusterCustomQuotasMatched...)

		for _, cq := range oldCustomQuotasMatched {
			typeName := getType(cq)

			if !slices.ContainsFunc(newCustomQuotasMatched, func(quota capsulev1beta2.CustomQuota) bool {
				return cq.Name == quota.Name
			}) {
				err := h.deleteResourceFromCustomQuota(ctx, oldObj, cq)
				if err != nil {
					h.log.Error(err, fmt.Sprintf("error deleting resource from %s %s", typeName, cq.Name))
				}

				continue
			}

			oldUsageValue, errOldUsageValue := customquotas.GetUsageFromUnstructured(oldObj, cq.Spec.Source.Path)
			newUsageValue, errNewUsageValue := customquotas.GetUsageFromUnstructured(newObj, cq.Spec.Source.Path)
			newUsage, errNewUsageParse := resource.ParseQuantity(newUsageValue)
			oldUsage, errOldUsageParse := resource.ParseQuantity(oldUsageValue)

			errNewUsage := errors.Join(errNewUsageValue, errNewUsageParse)
			if errNewUsage != nil {
				h.log.Error(errNewUsage, fmt.Sprintf("error getting usage from object for %s %s", typeName, cq.Name))

				newUsage = resource.MustParse("0")
			}

			if errOldUsageParse != nil {
				oldUsage = resource.MustParse("0")
			}

			if oldUsage.Cmp(newUsage) == 0 {
				continue
			}

			if errOldUsageValue != nil {
				cq.Status.Claims = append(cq.Status.Claims, fmt.Sprintf("%s.%s", req.Namespace, req.Name))
			}

			newUsed := cq.Status.Used.DeepCopy()
			newUsed.Sub(oldUsage)
			newUsed.Add(newUsage)

			if newUsed.Cmp(cq.Spec.Limit) == 1 {
				response := admission.Denied(fmt.Sprintf("updating resource exceeds limit for %s %s", typeName, cq.Name))

				return &response
			}

			cq.Status.Used.Sub(oldUsage)
			cq.Status.Used.Add(newUsage)
			cq.Status.Available = cq.Spec.Limit.DeepCopy()
			cq.Status.Available.Sub(cq.Status.Used)

			err = h.updateSubResStatusCustomQuota(ctx, cq)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error updating Sub-Resource for %s %s status", typeName, cq.Name))
			}
		}

		return nil
	}
}

func (h *customquotasHandler) deleteResourceFromCustomQuota(ctx context.Context, obj unstructured.Unstructured, cq capsulev1beta2.CustomQuota) error {
	typeName := getType(cq)
	claim := fmt.Sprintf("%s.%s", obj.GetNamespace(), obj.GetName())

	idx := slices.Index(cq.Status.Claims, claim)

	if idx == -1 {
		h.log.Info("claim not found in CustomQuota claims list, skipping deletion", "claim", claim, "customQuota", cq.Name)
	} else {
		cq.Status.Claims = slices.Delete(cq.Status.Claims, idx, idx+1)
	}

	usageValue, err := customquotas.GetUsageFromUnstructured(obj, cq.Spec.Source.Path)
	if err != nil {
		return fmt.Errorf("error getting usage from object for %s %s: %w", typeName, cq.Name, err)
	}

	usage, err := resource.ParseQuantity(usageValue)
	if err != nil {
		usage = resource.MustParse("0")
	}

	cq.Status.Used.Sub(usage)
	cq.Status.Available = cq.Spec.Limit.DeepCopy()
	cq.Status.Available.Sub(cq.Status.Used)

	return h.updateSubResStatusCustomQuota(ctx, cq)
}

func (h *customquotasHandler) getCustomQuotaMatched(ctx context.Context, req admission.Request, u unstructured.Unstructured) ([]capsulev1beta2.CustomQuota, error) {
	list := &capsulev1beta2.CustomQuotaList{}
	if err := h.client.List(ctx, list, client.InNamespace(req.Namespace)); err != nil {
		return nil, err
	}

	var customQuotasMatched []capsulev1beta2.CustomQuota

	for _, cq := range list.Items {
		gr, err := schema.ParseGroupVersion(cq.Spec.Source.Version)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error parsing GroupVersion for custom quota %s", cq.Name))

			continue
		}

		if cq.Spec.Source.Kind != req.Kind.Kind || gr.Version != req.Kind.Version || gr.Group != req.Kind.Group {
			continue
		}

		for _, selector := range cq.Spec.ScopeSelectors {
			sel, err := metav1.LabelSelectorAsSelector(&selector)
			if err != nil {
				h.log.Error(err, "error converting custom selector")

				continue
			}

			matches := sel.Matches(labels.Set(u.GetLabels()))
			if matches {
				customQuotasMatched = append(customQuotasMatched, cq)
			}
		}
	}

	return customQuotasMatched, nil
}

func (h *customquotasHandler) getClusterCustomQuotaMatched(ctx context.Context, req admission.Request, u unstructured.Unstructured) ([]capsulev1beta2.CustomQuota, error) {
	list := &capsulev1beta2.ClusterCustomQuotaList{}
	if err := h.client.List(ctx, list); err != nil {
		return nil, err
	}

	var customQuotasMatched []capsulev1beta2.CustomQuota

	for _, cq := range list.Items {
		gr, err := schema.ParseGroupVersion(cq.Spec.Source.Version)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error parsing GroupVersion for cluster custom quota %s", cq.Name))

			continue
		}

		if cq.Spec.Source.Kind != req.Kind.Kind || gr.Version != req.Kind.Version || gr.Group != req.Kind.Group {
			continue
		}

		var namespaces []string

		if namespaces, err = customquotas.GetNamespacesMatchingSelectors(ctx, cq.Spec.Selectors, h.client); err != nil {
			h.log.Error(err, fmt.Sprintf("error getting namespaces matching selectors for ClusterCustomQuota %s: %v", cq.Name, err))

			continue
		}

		if !slices.Contains(namespaces, req.Namespace) {
			continue
		}

		for _, selector := range cq.Spec.ScopeSelectors {
			sel, err := metav1.LabelSelectorAsSelector(&selector)
			if err != nil {
				h.log.Error(err, "error converting custom selector")

				continue
			}

			matches := sel.Matches(labels.Set(u.GetLabels()))
			if matches {
				customQuotasMatched = append(customQuotasMatched, capsulev1beta2.CustomQuota{
					ObjectMeta: cq.ObjectMeta,
					TypeMeta:   cq.TypeMeta,
					Spec:       cq.Spec.CustomQuotaSpec,
					Status:     cq.Status,
				})
			}
		}
	}

	return customQuotasMatched, nil
}

func (h *customquotasHandler) updateSubResStatusCustomQuota(ctx context.Context, cq capsulev1beta2.CustomQuota) error {
	if cq.Namespace != "" {
		if err := h.client.Status().Update(ctx, &cq); err != nil {
			return fmt.Errorf("error updating CustomQuota %s status: %w", cq.Name, err)
		}

		return nil
	}

	clusterCustomQuota := &capsulev1beta2.ClusterCustomQuota{
		TypeMeta:   cq.TypeMeta,
		ObjectMeta: cq.ObjectMeta,
		Spec:       capsulev1beta2.ClusterCustomQuotaSpec{CustomQuotaSpec: cq.Spec},
		Status:     cq.Status,
	}

	err := h.client.Status().Update(ctx, clusterCustomQuota)
	if err != nil {
		return fmt.Errorf("error updating ClusterCustomQuota %s status: %w", cq.Name, err)
	}

	return nil
}
