// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package handlers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type TypedHandlerWithTenantWithRuleset[T client.Object] interface {
	OnCreate(c client.Client, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, rule *capsulev1beta2.NamespaceRuleBody) Func
	OnUpdate(c client.Client, obj T, old T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, rule *capsulev1beta2.NamespaceRuleBody) Func
	OnDelete(c client.Client, obj T, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, rule *capsulev1beta2.NamespaceRuleBody) Func
}

type TypedTenantWithRulesetHandler[T client.Object] struct {
	Factory  NewObjectFunc[T]
	Handlers []TypedHandlerWithTenantWithRuleset[T]
}

func (h *TypedTenantWithRulesetHandler[T]) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
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

		rule, err := h.resolveRuleset(ctx, c, req, req.Namespace, tnt)
		if err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnCreate(c, obj, decoder, recorder, tnt, rule)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) OnUpdate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
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

		rule, err := h.resolveRuleset(ctx, c, req, req.Namespace, tnt)
		if err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnUpdate(c, oldObj, newObj, decoder, recorder, tnt, rule)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) OnDelete(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) Func {
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

		rule, err := h.resolveRuleset(ctx, c, req, req.Namespace, tnt)
		if err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnDelete(c, obj, decoder, recorder, tnt, rule)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) resolveTenant(ctx context.Context, c client.Client, req admission.Request) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	return tenant.TenantByStatusNamespace(ctx, c, req.Namespace)
}

// Resolve the corresponding managed ruleset for this namespace
// If not yet present try to calculate it.
func (h *TypedTenantWithRulesetHandler[T]) resolveRuleset(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	namespace string,
	tnt *capsulev1beta2.Tenant,
) (*capsulev1beta2.NamespaceRuleBody, error) {
	rs := &capsulev1beta2.RuleStatus{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      meta.NameForManagedRuleStatus(),
	}

	if err := c.Get(ctx, key, rs); err == nil {
		rule := rs.Status.Rule

		return &rule, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return nil, err
	}

	return tenant.BuildNamespaceRuleBodyForNamespace(ns, tnt)
}
