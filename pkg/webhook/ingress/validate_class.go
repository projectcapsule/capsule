// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type class struct {
	configuration configuration.Configuration
	version       *version.Version
}

func Class(configuration configuration.Configuration, version *version.Version) capsulewebhook.Handler {
	return &class{
		configuration: configuration,
		version:       version,
	}
}

func (r *class) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, r.version, client, req, decoder, recorder)
	}
}

func (r *class) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, r.version, client, req, decoder, recorder)
	}
}

func (r *class) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *class) validate(ctx context.Context, version *version.Version, client client.Client, req admission.Request, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
	ingress, err := FromRequest(req, decoder)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	var tnt *capsulev1beta2.Tenant

	tnt, err = TenantFromIngress(ctx, client, ingress)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.IngressOptions.AllowedClasses

	if allowed == nil {
		return nil
	}

	ingressClass := ingress.IngressClass()

	if ingressClass == nil {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingIngressClass", "Ingress %s/%s is missing IngressClass", req.Namespace, req.Name)

		response := admission.Denied(NewIngressClassUndefined(*allowed).Error())

		return &response
	}

	selector := false

	// Verify if the IngressClass exists and matches the label selector/expression
	if len(allowed.MatchExpressions) > 0 || len(allowed.MatchLabels) > 0 {
		ingressClassObj, err := utils.GetIngressClassByName(ctx, version, client, ingressClass)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		// Ingress Class is present, check if it matches the selector
		if ingressClassObj != nil {
			selector = allowed.SelectorMatch(ingressClassObj)
		}
	}

	switch {
	case allowed.MatchDefault(*ingressClass):
		return nil
	case allowed.Match(*ingressClass) || selector:
		return nil
	default:
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenIngressClass", "Ingress %s/%s IngressClass %s is forbidden for the current Tenant", req.Namespace, req.Name, &ingressClass)

		response := admission.Denied(NewIngressClassForbidden(*ingressClass, *allowed).Error())

		return &response
	}
}
