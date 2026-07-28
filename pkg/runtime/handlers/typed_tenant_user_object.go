// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type TypedHandlerWithTenantUser[T client.Object] interface {
	OnCreate(c client.Client, reader client.Reader, user users.AdmissionUser, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnUpdate(c client.Client, reader client.Reader, user users.AdmissionUser, obj T, old T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnDelete(c client.Client, reader client.Reader, user users.AdmissionUser, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
}

type TypedTenantWithUserHandler[T client.Object] struct {
	Factory       NewObjectFunc[T]
	Handlers      []TypedHandlerWithTenantUser[T]
	Configuration configuration.Configuration
	Predicate     func(req admission.Request, obj T, oldObj T) bool
	UserResolver  func(
		ctx context.Context,
		c client.Client,
		req admission.Request,
		cfg configuration.Configuration,
	) users.AdmissionUser
}

func (h *TypedTenantWithUserHandler[T]) OnCreate(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		obj := h.Factory()
		if err := decoder.Decode(req, obj); err != nil {
			return ErroredResponse(err)
		}

		if h.Predicate != nil {
			var oldObj T
			if !h.Predicate(req, obj, oldObj) {
				return nil
			}
		}

		tnt, err := h.resolveTenant(ctx, reader, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		user := h.resolveUser(ctx, c, req)

		for _, hndl := range h.Handlers {
			if response := hndl.OnCreate(c, reader, user, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithUserHandler[T]) OnUpdate(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		newObj := h.Factory()
		if err := decoder.Decode(req, newObj); err != nil {
			return ErroredResponse(err)
		}

		oldObj := h.Factory()
		if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
			return ErroredResponse(err)
		}

		if h.Predicate != nil && !h.Predicate(req, newObj, oldObj) {
			return nil
		}

		tnt, err := h.resolveTenant(ctx, reader, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		user := h.resolveUser(ctx, c, req)

		for _, hndl := range h.Handlers {
			if response := hndl.OnUpdate(c, reader, user, oldObj, newObj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithUserHandler[T]) OnDelete(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := h.resolveTenant(ctx, reader, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		obj := h.Factory()
		if err := decoder.Decode(req, obj); err != nil {
			return ErroredResponse(err)
		}

		user := h.resolveUser(ctx, c, req)

		for _, hndl := range h.Handlers {
			if response := hndl.OnDelete(c, reader, user, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithUserHandler[T]) resolveUser(
	ctx context.Context,
	c client.Client,
	req admission.Request,
) users.AdmissionUser {
	if h.UserResolver != nil {
		return h.UserResolver(ctx, c, req, h.Configuration)
	}

	return ResolveAdmissionUser(ctx, c, req, h.Configuration)
}

func (h *TypedTenantWithUserHandler[T]) resolveTenant(ctx context.Context, c client.Reader, req admission.Request) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	return tenant.GetTenantByNamespace(ctx, c, req.Namespace)
}
