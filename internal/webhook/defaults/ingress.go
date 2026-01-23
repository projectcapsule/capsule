// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	"net/http"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleingress "github.com/projectcapsule/capsule/internal/webhook/ingress"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

func mutateIngressDefaults(ctx context.Context, req admission.Request, version *version.Version, c client.Client, decoder admission.Decoder, namespace string) *admission.Response {
	ingress, err := capsuleingress.FromRequest(req, decoder)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	ingress.SetNamespace(namespace)

	var tnt *capsulev1beta2.Tenant

	tnt, err = capsuleingress.TenantFromIngress(ctx, c, ingress)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}
	// Validate Default Ingress
	allowed := tnt.Spec.IngressOptions.AllowedClasses

	if allowed == nil || allowed.Default == "" {
		return nil
	}

	var mutate bool

	var ingressClass client.Object

	if ingressClassName := ingress.IngressClass(); ingressClassName != nil && *ingressClassName != allowed.Default {
		if ingressClass, err = utils.GetIngressClassByName(ctx, version, c, ingressClassName); err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Denied(NewIngressClassError(*ingressClassName, err).Error())

			return &response
		}
	} else {
		mutate = true
	}

	if mutate = mutate || (utils.IsDefaultIngressClass(ingressClass) && ingressClass.GetName() != allowed.Default); !mutate {
		return nil
	}

	ingress.SetIngressClass(allowed.Default)
	// Marshal Manifest
	marshaled, err := json.Marshal(ingress)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
