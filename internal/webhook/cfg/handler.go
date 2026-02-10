// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func Handler(configuration configuration.Configuration, handlers ...handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration]) handlers.Handler {
	return &handler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration]
}

func (h *handler) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		config := &capsulev1beta2.CapsuleConfiguration{}
		if err := decoder.Decode(req, config); err != nil {
			return utils.ErroredResponse(err)
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, config, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnDelete(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		config := &capsulev1beta2.CapsuleConfiguration{}
		if err := decoder.Decode(req, config); err != nil {
			return utils.ErroredResponse(err)
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(c, config, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnUpdate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		config := &capsulev1beta2.CapsuleConfiguration{}
		if err := decoder.Decode(req, config); err != nil {
			return utils.ErroredResponse(err)
		}

		old := &capsulev1beta2.CapsuleConfiguration{}
		if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
			return utils.ErroredResponse(err)
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, config, old, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}
