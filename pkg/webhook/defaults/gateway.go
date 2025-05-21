// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulegateway "github.com/projectcapsule/capsule/pkg/webhook/gateway"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

func mutateGatewayDefaults(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, namespce string) *admission.Response {
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
			response := admission.Denied(NewGatewayError(gatewayObj.Spec.GatewayClassName, err).Error())

			return &response
		}
	}

	if gatewayClass != nil && gatewayClass.Name != allowed.Default {
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Denied(NewGatewayClassError(gatewayClass.Name, err).Error())

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

	recorder.Eventf(tnt, corev1.EventTypeNormal, "TenantDefault", "Assigned Tenant default Gateway Class %s to %s/%s", allowed.Default, gatewayObj.Name, gatewayObj.Namespace)

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
