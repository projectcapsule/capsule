// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type cordoningHandler struct{}

func CordoningHandler(configuration configuration.Configuration) handlers.Handler {
	return &cordoningHandler{}
}

func (h *cordoningHandler) OnCreate(
	c client.Client,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req)
	}
}

func (h *cordoningHandler) OnDelete(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req)
	}
}

func (h *cordoningHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req)
	}
}

func (h *cordoningHandler) cordonHandler(ctx context.Context, c client.Client, req admission.Request) *admission.Response {
	msg := fmt.Sprintf("The current namespace '%s' is cordoned. The attempted operation %s for %s/%s/%s/%s is not permitted during cordoning status.", req.Namespace, req.Operation, req.RequestKind.Group, req.RequestKind.Version, req.RequestKind.Kind, req.Name)

	response := admission.Denied(msg)

	return &response
}
