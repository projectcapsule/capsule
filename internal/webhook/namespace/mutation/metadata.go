// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type metadataHandler struct {
	cfg configuration.Configuration
}

func MetadataHandler(cfg configuration.Configuration) capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &metadataHandler{
		cfg: cfg,
	}
}

func (h *metadataHandler) OnCreate(client client.Client, ns *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tenant, errResponse := utils.GetNamespaceTenant(ctx, client, ns, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tenant == nil {
			response := admission.Denied("Unable to assign namespace to tenant.")

			return &response
		}

		// sync namespace metadata
		instance := tenant.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: ns.GetName(),
			UID:  ns.GetUID(),
		})

		if len(instance.Metadata.Labels) == 0 && len(instance.Metadata.Annotations) == 0 {
			return nil
		}

		labels := ns.GetLabels()
		for k, v := range instance.Metadata.Labels {
			labels[k] = v
		}

		ns.SetLabels(labels)

		annotations := ns.GetAnnotations()
		for k, v := range instance.Metadata.Annotations {
			annotations[k] = v
		}

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

func (h *metadataHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metadataHandler) OnUpdate(client.Client, *corev1.Namespace, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
