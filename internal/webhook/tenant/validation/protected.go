// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type protectedHandler struct{}

func ProtectedHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &protectedHandler{}
}

func (h *protectedHandler) OnCreate(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *protectedHandler) OnDelete(
	c client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if tnt.Spec.PreventDeletion {
			response := admission.Denied("tenant is protected and cannot be deleted")

			return &response
		}

		return nil
	}
}

func (h *protectedHandler) OnUpdate(client.Client, *capsulev1beta2.Tenant, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
