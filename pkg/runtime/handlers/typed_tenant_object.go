// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type TypedHandlerWithTenant[T client.Object] interface {
	OnCreate(c client.Client, reader client.Reader, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnUpdate(c client.Client, reader client.Reader, obj T, old T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnDelete(c client.Client, reader client.Reader, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
}

type TypedTenantHandler[T client.Object] struct {
	Factory   NewObjectFunc[T]
	Handlers  []TypedHandlerWithTenant[T]
	Predicate func(req admission.Request, obj T, oldObj T) bool
}

func (h *TypedTenantHandler[T]) OnCreate(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		var (
			obj     T
			decoded bool
		)

		if h.Predicate != nil {
			obj = h.Factory()
			if err := decoder.Decode(req, obj); err != nil {
				return ErroredResponse(err)
			}

			decoded = true

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

		if !decoded {
			obj = h.Factory()
			if err := decoder.Decode(req, obj); err != nil {
				return ErroredResponse(err)
			}
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnCreate(c, reader, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) OnUpdate(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		var (
			newObj  T
			oldObj  T
			decoded bool
		)

		if h.Predicate != nil {
			newObj = h.Factory()
			if err := decoder.Decode(req, newObj); err != nil {
				return ErroredResponse(err)
			}

			oldObj = h.Factory()
			if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
				return ErroredResponse(err)
			}

			decoded = true

			if !h.Predicate(req, newObj, oldObj) {
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

		if !decoded {
			newObj = h.Factory()
			if err := decoder.Decode(req, newObj); err != nil {
				return ErroredResponse(err)
			}

			oldObj = h.Factory()
			if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
				return ErroredResponse(err)
			}
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnUpdate(c, reader, oldObj, newObj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) OnDelete(c client.Client, reader client.Reader, decoder admission.Decoder, recorder events.EventRecorder) Func {
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

		for _, hndl := range h.Handlers {
			if response := hndl.OnDelete(c, reader, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) resolveTenant(ctx context.Context, c client.Reader, req admission.Request) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	return tenant.GetTenantByNamespace(ctx, c, req.Namespace)
}
