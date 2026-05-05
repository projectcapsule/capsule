// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulehttproute "github.com/projectcapsule/capsule/internal/webhook/httproute"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func mutateHTTPRouteDefaults(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, namespace string) *admission.Response {
	routeObj := &gatewayv1.HTTPRoute{}
	if err := decoder.Decode(req, routeObj); err != nil {
		return utils.ErroredResponse(err)
	}

	routeObj.SetNamespace(namespace)

	tnt, err := capsulehttproute.TenantFromHTTPRoute(ctx, c, routeObj)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	// Resolve namespace-level rules to find the gateway default.
	ns := &corev1.Namespace{}
	if err = c.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return utils.ErroredResponse(err)
	}

	ruleBody, err := tenant.BuildNamespaceRuleBodyForNamespace(ns, tnt)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if ruleBody == nil || ruleBody.Enforce.Gateways == nil || ruleBody.Enforce.Gateways.Gateway == nil {
		return nil
	}

	gwDefault := ruleBody.Enforce.Gateways.Gateway.Default
	if gwDefault == nil {
		return nil
	}

	// Only inject the default when the HTTPRoute has no parentRefs.
	if len(routeObj.Spec.ParentRefs) > 0 {
		return nil
	}

	defaultNamespace := gwDefault.Namespace
	if defaultNamespace == "" {
		defaultNamespace = namespace
	}

	ns16 := gatewayv1.Namespace(defaultNamespace)

	routeObj.Spec.ParentRefs = []gatewayv1.ParentReference{
		{
			Group:     groupPtr(gatewayv1.GroupName),
			Kind:      kindPtr("Gateway"),
			Name:      gatewayv1.ObjectName(gwDefault.Name),
			Namespace: &ns16,
		},
	}

	marshaled, err := json.Marshal(routeObj)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}

func groupPtr(g string) *gatewayv1.Group {
	v := gatewayv1.Group(g)

	return &v
}

func kindPtr(k string) *gatewayv1.Kind {
	v := gatewayv1.Kind(k)

	return &v
}
