// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type validating struct{}

func Validating() handlers.TypedHandlerWithTenant[*corev1.Service] {
	return &validating{}
}

func (h *validating) OnCreate(
	_ client.Client,
	_ client.Reader,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(req, recorder, svc, tnt)
	}
}

func (h *validating) OnUpdate(
	_ client.Client,
	_ client.Reader,
	old *corev1.Service,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(req, recorder, svc, tnt)
	}
}

func (h *validating) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Service,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) handle(
	req admission.Request,
	recorder events.EventRecorder,
	svc *corev1.Service,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if svc.Spec.Type == corev1.ServiceTypeNodePort && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.NodePort {
		recorder.Eventf(
			svc,
			tnt,
			corev1.EventTypeWarning,
			events.ReasonForbiddenNodePort,
			events.ActionValidationDenied,
			"Cannot be type of NodePort for the Tenant %s", tnt.GetName(),
		)

		return ad.Deny(caperrors.NewExternalNameDisabledError().Error())
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.ExternalName {
		recorder.Eventf(
			svc,
			tnt,
			corev1.EventTypeWarning,
			events.ReasonForbiddenExternalName,
			events.ActionValidationDenied,
			"Cannot be type of ExternalName for the Tenant %s", tnt.GetName(),
		)

		return ad.Deny(caperrors.NewExternalNameDisabledError().Error())
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.LoadBalancer {
		recorder.Eventf(
			svc,
			tnt,
			corev1.EventTypeWarning,
			events.ReasonForbiddenLoadBalancer,
			events.ActionValidationDenied,
			"Cannot be type of LoadBalancer for the Tenant %s", tnt.GetName(),
		)

		return ad.Deny(caperrors.NewLoadBalancerDisabled().Error())
	}

	if tnt.Spec.ServiceOptions != nil {
		err := api.ValidateForbidden(svc.Annotations, tnt.Spec.ServiceOptions.ForbiddenAnnotations)
		if err != nil {
			err = errors.Wrap(err, "annotations validation failed")

			recorder.Eventf(
				svc,
				tnt,
				corev1.EventTypeWarning,
				events.ReasonForbiddenAnnotation,
				events.ActionValidationDenied,
				err.Error(),
			)

			return ad.Deny(err.Error())
		}

		err = api.ValidateForbidden(svc.Labels, tnt.Spec.ServiceOptions.ForbiddenLabels)
		if err != nil {
			err = errors.Wrap(err, "labels validation failed")

			recorder.Eventf(
				svc,
				tnt,
				corev1.EventTypeWarning,
				events.ReasonForbiddenLabel,
				events.ActionValidationDenied,
				err.Error(),
			)

			return ad.Deny(err.Error())
		}
	}

	if svc.Spec.ExternalIPs == nil || (tnt.Spec.ServiceOptions == nil || tnt.Spec.ServiceOptions.ExternalServiceIPs == nil) {
		return nil
	}

	ipInCIDR := func(ip net.IP) bool {
		for _, allowed := range tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed {
			if !strings.Contains(string(allowed), "/") {
				allowed += "/32"
			}

			_, allowedIP, _ := net.ParseCIDR(string(allowed))

			if allowedIP.Contains(ip) {
				return true
			}
		}

		return false
	}

	for _, externalIP := range svc.Spec.ExternalIPs {
		ip := net.ParseIP(externalIP)

		if !ipInCIDR(ip) {
			recorder.Eventf(
				svc,
				tnt,
				corev1.EventTypeWarning,
				events.ReasonForbiddenExternalServiceIP,
				events.ActionValidationDenied,
				"External IP %s is forbidden for the Tenant %s", ip.String(), tnt.GetName(),
			)

			return ad.Deny(caperrors.NewExternalServiceIPForbidden(tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed).Error())
		}
	}

	return nil
}
