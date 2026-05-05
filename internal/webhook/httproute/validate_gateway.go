// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package httproute

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type gatewayValidator struct{}

// GatewayValidator returns a TypedHandlerWithTenantWithRuleset that validates
// HTTPRoute parentRefs against the gateway rules configured in namespace rules.
func GatewayValidator() handlers.TypedHandlerWithTenantWithRuleset[*gatewayv1.HTTPRoute] {
	return &gatewayValidator{}
}

func (h *gatewayValidator) OnCreate(
	c client.Client,
	obj *gatewayv1.HTTPRoute,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	rule *capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, req, obj, tnt, recorder, rule)
	}
}

func (h *gatewayValidator) OnUpdate(
	c client.Client,
	_ *gatewayv1.HTTPRoute,
	obj *gatewayv1.HTTPRoute,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	rule *capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, req, obj, tnt, recorder, rule)
	}
}

func (h *gatewayValidator) OnDelete(
	_ client.Client,
	_ *gatewayv1.HTTPRoute,
	_ admission.Decoder,
	_ events.EventRecorder,
	_ *capsulev1beta2.Tenant,
	_ *capsulev1beta2.NamespaceRuleBody,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *gatewayValidator) validate(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	route *gatewayv1.HTTPRoute,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	rule *capsulev1beta2.NamespaceRuleBody,
) *admission.Response {
	if rule == nil || rule.Enforce.Gateways == nil || rule.Enforce.Gateways.Gateway == nil {
		return nil
	}

	allowed := rule.Enforce.Gateways.Gateway

	for _, parentRef := range route.Spec.ParentRefs {
		// Only validate parentRefs that point to a Gateway resource.
		if parentRef.Kind != nil && *parentRef.Kind != gatewayv1.Kind("Gateway") {
			continue
		}

		if parentRef.Group != nil && string(*parentRef.Group) != gatewayv1.GroupName {
			continue
		}

		gwName := string(parentRef.Name)
		gwNamespace := route.Namespace

		if parentRef.Namespace != nil {
			gwNamespace = string(*parentRef.Namespace)
		}

		// Try to fetch the Gateway object for label-selector matching.
		gw := &gatewayv1.Gateway{}

		var gwObj client.Object

		if err := c.Get(ctx, types.NamespacedName{Namespace: gwNamespace, Name: gwName}, gw); err != nil {
			if !k8serrors.IsNotFound(err) {
				return errResponse(err)
			}
		} else {
			gwObj = gw
		}

		if !allowed.MatchGateway(gwNamespace, gwName, gwObj) {
			recorder.Eventf(
				tnt,
				nil,
				corev1.EventTypeWarning,
				evt.ReasonForbiddenGateway,
				evt.ActionValidationDenied,
				"HTTPRoute %s/%s references forbidden Gateway %s/%s",
				req.Namespace, req.Name, gwNamespace, gwName,
			)

			response := admission.Denied(
				caperrors.NewGatewayForbidden(gwName, gwNamespace, *allowed).Error(),
			)

			return &response
		}
	}

	return nil
}

func errResponse(err error) *admission.Response {
	resp := admission.Errored(500, err)

	return &resp
}
