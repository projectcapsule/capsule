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

	capsuletenant "github.com/projectcapsule/capsule/controllers/tenant"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type metadataHandler struct {
	cfg configuration.Configuration
}

func MetadataHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &metadataHandler{
		cfg: cfg,
	}
}

func (h *metadataHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}

		tenant, errResponse := getNamespaceTenant(ctx, client, ns, req, h.cfg, recorder)
		if errResponse != nil {
			return errResponse
		}

		if tenant == nil {
			response := admission.Denied("Unable to assign namespace to tenant.")

			return &response
		}

		// sync namespace metadata
		if err := capsuletenant.SyncNamespaceMetadata(tenant, ns); err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		marshaled, err := json.Marshal(ns)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func (h *metadataHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metadataHandler) OnUpdate(_ client.Client, _ admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return nil
	}
}
