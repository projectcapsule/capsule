// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type metadataHandler struct {
	cfg configuration.Configuration
}

func MetadataHandler(cfg configuration.Configuration) handlers.TypedHandler[*corev1.Namespace] {
	return &metadataHandler{
		cfg: cfg,
	}
}

func (h *metadataHandler) OnCreate(client client.Client, ns *corev1.Namespace, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, client, ns, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			response := admission.Denied("Unable to assign namespace to tenant.")

			return &response
		}

		labels, annotations, err := tenant.BuildNamespaceMetadataForTenant(ns, tnt)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		managedMetadataOnly := tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.ManagedMetadataOnly
		if managedMetadataOnly {
			labels = mergeStringMap(ns.GetLabels(), labels)
			annotations = mergeStringMap(ns.GetAnnotations(), annotations)
		}

		tenant.AddNamespaceNameLabels(labels, ns)
		tenant.AddTenantNameLabel(labels, ns, tnt)

		ns.SetLabels(labels)
		ns.SetAnnotations(annotations)

		marshaled, err := json.Marshal(ns)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func (h *metadataHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metadataHandler) OnUpdate(c client.Client, newNs *corev1.Namespace, oldNs *corev1.Namespace, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, errResponse := utils.GetNamespaceTenant(ctx, c, oldNs, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tnt == nil {
			response := admission.Denied("Unable to assign namespace to tenant.")

			return &response
		}

		o, err := json.Marshal(newNs.DeepCopy())
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		labels, annotations, err := tenant.BuildNamespaceMetadataForTenant(newNs, tnt)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		managedMetadataOnly := tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.ManagedMetadataOnly
		if !managedMetadataOnly {
			labels = mergeStringMap(newNs.GetLabels(), labels)
			annotations = mergeStringMap(newNs.GetAnnotations(), annotations)
		}

		tenant.AddNamespaceNameLabels(labels, oldNs)
		tenant.AddTenantNameLabel(labels, oldNs, tnt)

		newNs.SetLabels(labels)
		newNs.SetAnnotations(annotations)

		obj, err := json.Marshal(newNs)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(o, obj)

		return &response
	}
}

func mergeStringMap(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}

	if dst == nil {
		return maps.Clone(src)
	}

	maps.Copy(dst, src)

	return dst
}
