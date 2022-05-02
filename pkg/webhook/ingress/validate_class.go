// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type class struct {
	configuration configuration.Configuration
}

func Class(configuration configuration.Configuration) capsulewebhook.Handler {
	return &class{configuration: configuration}
}

// nolint:dupl
func (r *class) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		if err = r.validateClass(*tenant, ingress.IngressClass()); err == nil {
			return nil
		}

		var forbiddenErr *ingressClassForbiddenError

		if errors.As(err, &forbiddenErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassForbidden", "Ingress %s/%s class is forbidden", ingress.Namespace(), ingress.Name())
		}

		var invalidErr *ingressClassNotValidError

		if errors.As(err, &invalidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassNotValid", "Ingress %s/%s class is invalid", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

// nolint:dupl
func (r *class) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		if err = r.validateClass(*tenant, ingress.IngressClass()); err == nil {
			return nil
		}

		var forbiddenErr *ingressClassForbiddenError

		if errors.As(err, &forbiddenErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassForbidden", "Ingress %s/%s class is forbidden", ingress.Namespace(), ingress.Name())
		}

		var invalidErr *ingressClassNotValidError

		if errors.As(err, &invalidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressClassNotValid", "Ingress %s/%s class is invalid", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

func (r *class) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *class) validateClass(tenant capsulev1beta1.Tenant, ingressClass *string) error {
	if tenant.Spec.IngressOptions.AllowedClasses == nil {
		return nil
	}

	if ingressClass == nil {
		return NewIngressClassNotValid(*tenant.Spec.IngressOptions.AllowedClasses)
	}

	var valid, matched bool

	if len(tenant.Spec.IngressOptions.AllowedClasses.Exact) > 0 {
		valid = tenant.Spec.IngressOptions.AllowedClasses.ExactMatch(*ingressClass)
	}

	matched = tenant.Spec.IngressOptions.AllowedClasses.RegexMatch(*ingressClass)

	if !valid && !matched {
		return NewIngressClassForbidden(*ingressClass, *tenant.Spec.IngressOptions.AllowedClasses)
	}

	return nil
}
