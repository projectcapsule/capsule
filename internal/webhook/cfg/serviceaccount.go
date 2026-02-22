// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type serviceAccountHandler struct{}

func ServiceAccountHandler() handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration] {
	return &serviceAccountHandler{}
}

func (h *serviceAccountHandler) OnCreate(
	_ client.Client,
	cfg *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *serviceAccountHandler) OnDelete(client.Client, *capsulev1beta2.CapsuleConfiguration, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *serviceAccountHandler) OnUpdate(
	_ client.Client,
	cfg *capsulev1beta2.CapsuleConfiguration,
	old *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *serviceAccountHandler) handle(config *capsulev1beta2.CapsuleConfiguration, req admission.Request) *admission.Response {
	nameSet := config.Spec.Impersonation.GlobalDefaultServiceAccount != ""
	nsSet := config.Spec.Impersonation.GlobalDefaultServiceAccountNamespace != ""

	if nameSet != nsSet {
		response := admission.Denied("both globalDefaultServiceAccount and globalDefaultServiceAccountNamespace must be set together")

		return &response
	}

	return nil
}
