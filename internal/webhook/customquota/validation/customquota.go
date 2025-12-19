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
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
			h.log.Error(err, fmt.Sprintf("error getting unstrutured: %v", err))

			return nil
		}

		customQuotasMatched, errNamespaced := h.getCustomQuotaMatched(ctx, req, u)
		clusterCustomQuotasMatched, errCluster := h.getClusterCustomQuotaMatched(ctx, req, u)

		err = errors.Join(errNamespaced, errCluster)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error getting matched CustomQuotas: %v", err))

			return nil
		}

		customQuotasMatched = append(customQuotasMatched, clusterCustomQuotasMatched...)

		for _, cq := range customQuotasMatched {
			typeName := getType(cq)
			claimList := cq.Status.Claims
			claimList = append(claimList, fmt.Sprintf("%s.%s", req.Namespace, req.Name))
			cq.Status.Claims = claimList

			usage, err := customquotas.GetUsageFromUnstructured(u, cq.Spec.Source.Path)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error getting usage from object for %s %s: %v", typeName, cq.Name, err))

				continue
			}

			newUsed := cq.Status.Used.DeepCopy()
			newUsed.Add(resource.MustParse(usage))

			if newUsed.Cmp(cq.Spec.Limit) == 1 {
				response := admission.Denied(fmt.Sprintf("updating resource exceeds limit for %s %s", typeName, cq.Name))

				return &response
			}

			cq.Status.Used.Add(resource.MustParse(usage))
			cq.Status.Available.Sub(resource.MustParse(usage))

			err = h.updateSubResStatusCustomQuota(ctx, cq)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error updating Sub-Resource for %s %s status: %v", typeName, cq.Name, err))
			}
		}

		return nil
	}
}

func (h *customquotasHandler) OnDelete(c client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		obj, err := getUnstructured(req.OldObject)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error getting unstrutured: %v", err))

			return nil
		}

		customQuotasMatched, errNamespaced := h.getCustomQuotaMatched(ctx, req, obj)
		clusterCustomQuotasMatched, errCluster := h.getClusterCustomQuotaMatched(ctx, req, obj)

		err = errors.Join(errNamespaced, errCluster)
		if err != nil {
			h.log.Error(err, fmt.Sprintf("error getting matched CustomQuotas: %v", err))

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
				h.log.Error(err, fmt.Sprintf("error deleting resource from %s %s: %v", typeName, cq.Name, err))
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
					h.log.Error(err, fmt.Sprintf("error deleting resource from %s %s: %v", typeName, cq.Name, err))
				}

				continue
			}

			oldUsage, errOldUsage := customquotas.GetUsageFromUnstructured(oldObj, cq.Spec.Source.Path)

			newUsage, errNewUsage := customquotas.GetUsageFromUnstructured(newObj, cq.Spec.Source.Path)
			if errNewUsage != nil {
				h.log.Error(errNewUsage, fmt.Sprintf("error getting usage from object for %s %s: %v", typeName, cq.Name, err))

				newUsage = "0"
			}

			if oldUsage == newUsage {
				continue
			}

			if errOldUsage != nil {
				oldUsage = "0"
				claimList := cq.Status.Claims
				claimList = append(claimList, fmt.Sprintf("%s.%s", req.Namespace, req.Name))
				cq.Status.Claims = claimList
			}

			newUsed := cq.Status.Used.DeepCopy()
			newUsed.Sub(resource.MustParse(oldUsage))
			newUsed.Add(resource.MustParse(newUsage))

			if newUsed.Cmp(cq.Spec.Limit) == 1 {
				response := admission.Denied(fmt.Sprintf("updating resource exceeds limit for %s %s", typeName, cq.Name))

				return &response
			}

			cq.Status.Used.Sub(resource.MustParse(oldUsage))
			cq.Status.Available.Add(resource.MustParse(oldUsage))
			cq.Status.Used.Add(resource.MustParse(newUsage))
			cq.Status.Available.Sub(resource.MustParse(newUsage))

			err = h.updateSubResStatusCustomQuota(ctx, cq)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error updating Sub-Resource for %s %s status: %v", typeName, cq.Name, err))
			}
		}

		return nil
	}
}

func (h *customquotasHandler) deleteResourceFromCustomQuota(ctx context.Context, obj unstructured.Unstructured, cq capsulev1beta2.CustomQuota) error {
	typeName := getType(cq)
	claim := fmt.Sprintf("%s.%s", obj.GetNamespace(), obj.GetName())
	claimList := cq.Status.Claims
	claimList = slices.Delete(claimList, slices.Index(claimList, claim), slices.Index(claimList, claim)+1)
	cq.Status.Claims = claimList

	usage, err := customquotas.GetUsageFromUnstructured(obj, cq.Spec.Source.Path)
	if err != nil {
		return fmt.Errorf("error getting usage from object for %s %s: %w", typeName, cq.Name, err)
	}

	cq.Status.Used.Sub(resource.MustParse(usage))
	cq.Status.Available.Add(resource.MustParse(usage))

	return h.updateSubResStatusCustomQuota(ctx, cq)
}

func (h *customquotasHandler) getCustomQuotaMatched(ctx context.Context, req admission.Request, u unstructured.Unstructured) ([]capsulev1beta2.CustomQuota, error) {
	list := &capsulev1beta2.CustomQuotaList{}
	if err := h.client.List(ctx, list, client.InNamespace(req.Namespace)); err != nil {
		return nil, err
	}

	var customQuotasMatched []capsulev1beta2.CustomQuota

	for _, cq := range list.Items {
		if cq.Spec.Source.Kind != req.Kind.Kind && cq.Spec.Source.Version != req.Kind.Version {
			continue
		}

		for _, selector := range cq.Spec.ScopeSelectors {
			sel, err := metav1.LabelSelectorAsSelector(&selector)
			if err != nil {
				h.log.Error(err, fmt.Sprintf("error converting custom selector: %v", err))

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
		if cq.Spec.Source.Kind != req.Kind.Kind && cq.Spec.Source.Version != req.Kind.Version {
			continue
		}

		var namespaces []string

		var err error

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
				h.log.Error(err, fmt.Sprintf("error converting custom selector: %v", err))

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

	return u, err
}
