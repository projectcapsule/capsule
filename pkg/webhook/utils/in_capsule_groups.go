// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/webhook"
)

func InCapsuleGroups(configuration configuration.Configuration, handlers ...webhook.Handler) webhook.Handler {
	return &handler{
		configuration: configuration,
		handlers:      handlers,
	}
}

type handler struct {
	configuration configuration.Configuration
	handlers      []webhook.Handler
}

//nolint:dupl
func (h *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !IsCapsuleUser(ctx, req, client, h.configuration.UserNames(), h.configuration.UserGroups(), h.configuration.IgnoreUserWithGroups()) {
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
func (h *handler) OnDelete(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !IsCapsuleUser(ctx, req, client, h.configuration.UserNames(), h.configuration.UserGroups(), h.configuration.IgnoreUserWithGroups()) {
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
func (h *handler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !IsCapsuleUser(ctx, req, client, h.configuration.UserNames(), h.configuration.UserGroups(), h.configuration.IgnoreUserWithGroups()) {
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
