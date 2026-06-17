// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type ownersHandler struct{}

func OwnersHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &ownersHandler{}
}

func (h *ownersHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return h.handle(tnt)
	}
}

func (h *ownersHandler) OnDelete(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if tnt.Spec.PreventDeletion {
			return ad.Deny("tenant is protected and cannot be deleted")
		}

		return nil
	}
}

func (h *ownersHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return h.handle(tnt)
	}
}

func (h *ownersHandler) handle(
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	for _, owner := range tnt.Spec.Owners {
		if err := tenant.ValidateTenantOwner(owner.UserSpec); err != nil {
			return ad.Deny(
				err.Error(),
			)
		}
	}

	return nil
}
