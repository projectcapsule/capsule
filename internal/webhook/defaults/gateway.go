// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	"net/http"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulegateway "github.com/projectcapsule/capsule/internal/webhook/gateway"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
)

func mutateGatewayDefaults(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, namespce string) *admission.Response {
	gatewayObj := &gatewayv1.Gateway{}
	if err := decoder.Decode(req, gatewayObj); err != nil {
		return utils.ErroredResponse(err)
	}

	gatewayObj.SetNamespace(namespce)

	tnt, err := capsulegateway.TenantFromGateway(ctx, c, gatewayObj)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.GatewayOptions.AllowedClasses

	if allowed == nil || allowed.Default == "" {
		return nil
	}

	var mutate bool

	gatewayClass, err := utils.GetGatewayClassClassByObjectName(ctx, c, gatewayObj.Spec.GatewayClassName)

	if gatewayClass == nil {
		if gatewayObj.Spec.GatewayClassName == ("") {
			mutate = true
		} else {
			response := admission.Denied(caperrors.NewGatewayError(gatewayObj.Spec.GatewayClassName, err).Error())

			return &response
		}
	}

	if gatewayClass != nil && gatewayClass.Name != allowed.Default {
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Denied(caperrors.NewGatewayClassError(gatewayClass.Name, err).Error())

			return &response
		}
	} else {
		mutate = true
	}

	if mutate = mutate || (gatewayClass.Name == allowed.Default); !mutate {
		return nil
	}

	gatewayObj.Spec.GatewayClassName = gatewayv1.ObjectName(allowed.Default)

	marshaled, err := json.Marshal(gatewayObj)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
