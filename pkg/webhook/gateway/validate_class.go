// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type class struct {
	configuration configuration.Configuration
}

func Class(configuration configuration.Configuration) capsulewebhook.Handler {
	return &class{
		configuration: configuration,
	}
}

func (r *class) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, client, req, decoder, recorder)
	}
}

func (r *class) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, client, req, decoder, recorder)
	}
}

func (r *class) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *class) validate(ctx context.Context, client client.Client, req admission.Request, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	gatewayObj := &gatewayv1.Gateway{}
	if err := decoder.Decode(req, gatewayObj); err != nil {
		return utils.ErroredResponse(err)
	}

	var tnt *capsulev1beta2.Tenant

	tnt, err := TenantFromGateway(ctx, client, gatewayObj)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.GatewayOptions.AllowedClasses

	if allowed == nil {
		return nil
	}

	gatewayClass, err := utils.GetGatewayClassClassByObjectName(ctx, client, gatewayObj.Spec.GatewayClassName)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if gatewayClass == nil {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingGatewayClass", "Gateway %s/%s is missing GatewayClass", req.Namespace, req.Name)

		response := admission.Denied(NewGatewayClassUndefined(*allowed).Error())

		return &response
	}

	selector := false
	// Verify if the GatewayClass exists and matches the label selector/expression
	if len(allowed.MatchExpressions) > 0 || len(allowed.MatchLabels) > 0 {
		gatewayClassObj, err := utils.GetGatewayClassClassByObjectName(ctx, client, gatewayObj.Spec.GatewayClassName)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		// Gateway Class is present, check if it matches the selector
		if gatewayClassObj != nil {
			selector = allowed.SelectorMatch(gatewayClassObj)
		}
	}

	switch {
	case allowed.MatchDefault(gatewayClass.Name):
		return nil
	case allowed.Match(gatewayClass.Name) || selector:
		return nil
	default:
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenGatewaClass", "Gateway %s/%s GatewayClass %s is forbidden for the current Tenant", req.Namespace, req.Name, &gatewayClass)

		response := admission.Denied(NewGatewayClassForbidden(gatewayObj.Name, *allowed).Error())

		return &response
	}
}
