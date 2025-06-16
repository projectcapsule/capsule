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
	"github.com/projectcapsule/capsule/controllers/resources"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type globalMutatingHandler struct {
	configuration configuration.Configuration
}

func GlobalMutatingHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &globalMutatingHandler{
		configuration: configuration,
	}
}

func (h *globalMutatingHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *globalMutatingHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(req, decoder)
	}
}

func (h *globalMutatingHandler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(req, decoder)
	}
}

func (h *globalMutatingHandler) handler(req admission.Request, decoder admission.Decoder) *admission.Response {
	resource := &capsulev1beta2.GlobalTenantResource{}
	if err := decoder.Decode(req, resource); err != nil {
		return utils.ErroredResponse(err)
	}

	changed := resources.SetGlobalTenantResourceServiceAccount(h.configuration, resource)
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
