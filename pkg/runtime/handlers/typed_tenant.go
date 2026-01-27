// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package handlers

import (
	"context"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type TypedHandlerWithTenant[T client.Object] interface {
	OnCreate(c client.Client, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnUpdate(c client.Client, obj T, old T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnDelete(c client.Client, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant) Func
}

type TypedTenantHandler[T client.Object] struct {
	Factory  NewObjectFunc[T]
	Handlers []TypedHandlerWithTenant[T]
}

func (h *TypedTenantHandler[T]) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := h.resolveTenant(ctx, c, req)
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
			if response := hndl.OnCreate(c, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) OnUpdate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := h.resolveTenant(ctx, c, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		newObj := h.Factory()
		if err := decoder.Decode(req, newObj); err != nil {
			return ErroredResponse(err)
		}

		oldObj := h.Factory()
		if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnUpdate(c, oldObj, newObj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) OnDelete(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := h.resolveTenant(ctx, c, req)
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
			if response := hndl.OnDelete(c, obj, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantHandler[T]) resolveTenant(ctx context.Context, c client.Client, req admission.Request) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	return tenant.TenantByStatusNamespace(ctx, c, req.Namespace)
}
