// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type metaHandler struct{}

func MetaHandler() capsulewebhook.Handler {
	return &metaHandler{}
}

func (h *metaHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *metaHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *metaHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metaHandler) handle(decoder admission.Decoder, req admission.Request) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	labels := tenant.GetLabels()
	if val, ok := labels[capsuleapi.TenantNameLabel]; ok && val == tenant.Name {
		return nil
	}

	if labels == nil {
		labels = make(map[string]string)
	}

	labels[capsuleapi.TenantNameLabel] = tenant.Name
	tenant.SetLabels(labels)

	marshaled, err := json.Marshal(tenant)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
