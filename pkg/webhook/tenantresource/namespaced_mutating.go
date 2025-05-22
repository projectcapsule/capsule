// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"context"
	"encoding/json"
	"net/http"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	caputils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type namespacedMutatingHandler struct {
	cfg configuration.Configuration
}

func NamespacedMutatingHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &namespacedMutatingHandler{}
}

func (h *namespacedMutatingHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespacedMutatingHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, decoder, recorder)
	}
}

func (h *namespacedMutatingHandler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, decoder, recorder)
	}
}

func (h *namespacedMutatingHandler) handler(ctx context.Context, clt client.Client, req admission.Request, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	resource := &capsulev1beta2.TenantResource{}
	if err := decoder.Decode(req, resource); err != nil {
		return utils.ErroredResponse(err)
	}

	changed := h.handleServiceAccount(resource)
	if !changed {
		return nil
	}

	// Marshal Manifest
	marshaled, err := json.Marshal(resource)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}
	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}

func (h *namespacedMutatingHandler) handleServiceAccount(resource *capsulev1beta2.TenantResource) (changed bool) {
	changed = false

	if resource.Spec.ServiceAccountName != "" {
		return
	}

	cfg := h.cfg.ServiceAccountClientProperties()
	if cfg == nil || cfg.TenantDefaultServiceAccount != "" {
		return
	}

	changed = true

	resource.Spec.ServiceAccountName = caputils.NamespacedServiceAccountName(cfg.TenantDefaultServiceAccount, resource.Namespace)

	return
}
