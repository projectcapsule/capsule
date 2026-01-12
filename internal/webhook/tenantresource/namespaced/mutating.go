// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package mutating

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type namespacedMutatingHandler struct {
	configuration configuration.Configuration
}

func NamespacedMutatingHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &namespacedMutatingHandler{
		configuration: configuration,
	}
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

	return nil
}
