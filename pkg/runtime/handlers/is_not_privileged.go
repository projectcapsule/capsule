// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func IsNotPrivileged(configuration configuration.Configuration, handlers ...Handler) Handler {
	return &isNotPrivileged{
		configuration: configuration,
		handlers:      handlers,
	}
}

type isNotPrivileged struct {
	configuration configuration.Configuration
	handlers      []Handler
}

func (h *isNotPrivileged) OnCreate(client client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if users.IsAdminUser(req, h.configuration.Administrators()) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(client, reader, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *isNotPrivileged) OnDelete(client client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if users.IsAdminUser(req, h.configuration.Administrators()) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(client, reader, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *isNotPrivileged) OnUpdate(client client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if users.IsAdminUser(req, h.configuration.Administrators()) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(client, reader, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}
