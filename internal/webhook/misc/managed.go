// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type managedValidatingHandler struct{}

func ManagedValidatingHandler() handlers.Handler {
	return &managedValidatingHandler{}
}

func (h *managedValidatingHandler) OnCreate(c client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *managedValidatingHandler) OnDelete(client client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		response := admission.Denied(fmt.Sprintf("resource %s is managed by capsule and can not by modified by capsule users", req.Name))

		return &response
	}
}

func (h *managedValidatingHandler) OnUpdate(client client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		response := admission.Denied(fmt.Sprintf("resource %s is managed by capsule and can not by modified by capsule users", req.Name))

		return &response
	}
}
