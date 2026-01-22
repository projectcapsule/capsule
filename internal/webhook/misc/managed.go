// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type managedValidatingHandler struct{}

func ManagedValidatingHandler() capsulewebhook.Handler {
	return &managedValidatingHandler{}
}

func (h *managedValidatingHandler) OnCreate(client.Client, admission.Decoder, events.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *managedValidatingHandler) OnDelete(client client.Client, _ admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *managedValidatingHandler) OnUpdate(client client.Client, _ admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *managedValidatingHandler) handler(ctx context.Context, clt client.Client, req admission.Request, recorder events.EventRecorder) *admission.Response {
	response := admission.Denied(fmt.Sprintf("resource %s is managed by capsule and can not by modified by capsule users", req.Name))

	return &response
}
