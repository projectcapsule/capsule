// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

func CapsuleAdministrator(configuration configuration.Configuration, handlers ...webhook.Handler) webhook.Handler {
	return &adminHandler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type adminHandler struct {
	cfg      configuration.Configuration
	handlers []webhook.Handler
}

//nolint:dupl
func (h *adminHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsAdminUser(req, h.cfg.Administrators()) {
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
func (h *adminHandler) OnDelete(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsAdminUser(req, h.cfg.Administrators()) {
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
func (h *adminHandler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsAdminUser(req, h.cfg.Administrators()) {
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

func isAdminUser()
