// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package owners

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

type ownerSubjectHandler struct{}

func UserMetadataHandler() handlers.Handler {
	return &ownerSubjectHandler{}
}

func (r *ownerSubjectHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		owner := &capsulev1beta2.TenantOwner{}
		if err := decoder.Decode(req, owner); err != nil {
			return ad.ErroredResponse(err)
		}

		return r.handle(owner)
	}
}

func (r *ownerSubjectHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		owner := &capsulev1beta2.TenantOwner{}
		if err := decoder.Decode(req, owner); err != nil {
			return ad.ErroredResponse(err)
		}

		return r.handle(owner)
	}
}

func (r *ownerSubjectHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ownerSubjectHandler) handle(
	owner *capsulev1beta2.TenantOwner,
) *admission.Response {
	if err := tenant.ValidateTenantOwner(owner.Spec.CoreOwnerSpec); err != nil {
		return ad.Deny(
			err.Error(),
		)
	}

	return nil
}
