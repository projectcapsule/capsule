// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type matchingHandler struct {
	predicate func(admission.Request) bool
	handler   Handler
}

// MatchingRequest filters requests before decoding and tenant/ruleset resolution.
// A skipped request continues the surrounding admission chain.
func MatchingRequest(predicate func(admission.Request) bool, handler Handler) Handler {
	return &matchingHandler{
		predicate: predicate,
		handler:   handler,
	}
}

func (h *matchingHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) Func {
	next := h.handler.OnCreate(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) Func {
	next := h.handler.OnUpdate(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) Func {
	next := h.handler.OnDelete(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) handle(next Func) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !h.predicate(req) {
			return nil
		}

		return next(ctx, req)
	}
}
