// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type managedValidatingHandler struct {
	configuration configuration.Configuration
}

func ManagedValidatingHandler(configuration configuration.Configuration) handlers.Handler {
	return &managedValidatingHandler{
		configuration: configuration,
	}
}

func (h *managedValidatingHandler) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) OnDelete(
	c client.Client,
	_ client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) OnUpdate(
	c client.Client,
	_ client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) handle(
	ctx context.Context,
	req admission.Request,
	c client.Client,
) *admission.Response {
	user := handlers.ResolveAdmissionUser(ctx, c, req, h.configuration)

	if user.IsAdmin() {
		return nil
	}

	return ad.Deny("Labeling resources as controller managed can only be done by the controller or administrators")
}
