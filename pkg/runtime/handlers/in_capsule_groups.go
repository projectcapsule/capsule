// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package handlers

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func InCapsuleGroups(configuration configuration.Configuration, handlers ...Handler) Handler {
	return &handler{
		configuration: configuration,
		handlers:      handlers,
	}
}

type handler struct {
	configuration configuration.Configuration
	handlers      []Handler
}

//nolint:dupl
func (h *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, client, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(client, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:dupl
func (h *handler) OnDelete(client client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, client, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(client, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:dupl
func (h *handler) OnUpdate(client client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, client, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(client, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}
