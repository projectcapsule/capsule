// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func NamespaceHandler(configuration configuration.Configuration, handlers ...handlers.TypedHandlerWithUser[*corev1.Namespace]) handlers.Handler {
	return &handler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []handlers.TypedHandlerWithUser[*corev1.Namespace]
}

func (h *handler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		if !user.IsAdmin() && !user.IsCapsule() {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		tnt, err := tenant.GetTenantByLabels(ctx, reader, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if tnt == nil && user.IsAdmin() {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, reader, user, ns, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		if !user.IsAdmin() && !user.IsCapsule() {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, reader, user, ns, oldNs, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
