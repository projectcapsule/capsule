// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type metadataHandler struct {
	cfg configuration.Configuration
}

func MetadataHandler(cfg configuration.Configuration) handlers.TypedHandlerWithUser[*corev1.Namespace] {
	return &metadataHandler{
		cfg: cfg,
	}
}

func (h *metadataHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, reader, c, ns, user, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			return nil
		}

		labels, annotations, err := tenant.BuildNamespaceMetadataForTenant(ns, tnt)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		managedMetadataOnly := tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.ManagedMetadataOnly
		if !managedMetadataOnly {
			labels = mergeStringMap(ns.GetLabels(), labels)
			annotations = mergeStringMap(ns.GetAnnotations(), annotations)
		}

		tenant.AddNamespaceNameLabels(labels, ns)
		tenant.AddTenantNameLabel(labels, tnt)

		ns.SetLabels(labels)
		ns.SetAnnotations(annotations)

		if response := h.handleCordoning(tnt, ns); response != nil {
			return response
		}

		return nil
	}
}

func (h *metadataHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metadataHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	user users.AdmissionUser,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := h.resolveTenantForUpdate(ctx, reader, c, oldNs, newNs, user, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			return nil
		}

		labels, annotations, err := tenant.BuildNamespaceMetadataForTenant(newNs, tnt)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		managedMetadataOnly := tnt.Spec.NamespaceOptions != nil &&
			tnt.Spec.NamespaceOptions.ManagedMetadataOnly

		if !managedMetadataOnly {
			labels = mergeStringMap(newNs.GetLabels(), labels)
			annotations = mergeStringMap(newNs.GetAnnotations(), annotations)
		}

		tenant.AddNamespaceNameLabels(labels, newNs)
		tenant.AddTenantNameLabel(labels, tnt)

		newNs.SetLabels(labels)
		newNs.SetAnnotations(annotations)

		return nil
	}
}

func mergeStringMap(dst, src map[string]string) map[string]string {
	out := maps.Clone(dst)
	if out == nil {
		out = map[string]string{}
	}

	maps.Copy(out, src)

	return out
}

func (h *metadataHandler) handleCordoning(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) *admission.Response {
	condition := tnt.Status.Conditions.GetConditionByType(meta.CordonedCondition)
	if condition == nil {
		return nil
	}

	if condition.Status != metav1.ConditionTrue {
		return nil
	}

	labels := ns.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	if _, ok := labels[meta.CordonedLabel]; ok {
		return nil
	}

	labels[meta.CordonedLabel] = meta.ValueTrue
	ns.SetLabels(labels)

	return nil
}

func (h *metadataHandler) resolveTenantForUpdate(
	ctx context.Context,
	reader client.Reader,
	cache client.Client,
	oldNs *corev1.Namespace,
	newNs *corev1.Namespace,
	user users.AdmissionUser,
	recorder events.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	if user.IsAdmin() {
		tnt, err := tenant.GetTenantByLabels(ctx, reader, newNs)
		if err != nil {
			return nil, ad.ErroredResponse(err)
		}

		if tnt != nil {
			return tnt, nil
		}

		tnt, err = tenant.GetTenantByLabels(ctx, reader, oldNs)
		if err != nil {
			return nil, ad.ErroredResponse(err)
		}

		return tnt, nil
	}

	tnt, errResponse := utils.GetNamespaceTenant(ctx, reader, cache, oldNs, user, h.cfg, recorder)
	if errResponse != nil {
		return nil, errResponse
	}

	return tnt, nil
}
