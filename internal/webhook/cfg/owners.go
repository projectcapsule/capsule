// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type ownerHandler struct{}

func OwnerHandler() handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration] {
	return &ownerHandler{}
}

func (h *ownerHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	cfg *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *ownerHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.CapsuleConfiguration,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	cfg *capsulev1beta2.CapsuleConfiguration,
	old *capsulev1beta2.CapsuleConfiguration,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *ownerHandler) handle(config *capsulev1beta2.CapsuleConfiguration, req admission.Request) *admission.Response {
	for _, owner := range config.Spec.Users {
		if err := tenant.ValidateTenantOwner(owner); err != nil {
			return ad.Deny(
				err.Error(),
			)
		}
	}

	return nil
}
