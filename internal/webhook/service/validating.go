// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

type validating struct{}

func Validating() capsulewebhook.TypedHandlerWithTenant[*corev1.Service] {
	return &validating{}
}

func (h *validating) OnCreate(
	c client.Client,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(req, recorder, svc, tnt)
	}
}

func (h *validating) OnUpdate(
	c client.Client,
	old *corev1.Service,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(req, recorder, svc, tnt)
	}
}

func (h *validating) OnDelete(
	client.Client,
	*corev1.Service,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
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
			evt.ReasonForbiddenNodePort,
			evt.ActionValidationDenied,
			"Cannot be type of NodePort for the Tenant %s", tnt.GetName(),
		)

		response := admission.Denied(NewNodePortDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.ExternalName {
		recorder.Eventf(
			svc,
			tnt,
			corev1.EventTypeWarning,
			evt.ReasonForbiddenExternalName,
			evt.ActionValidationDenied,
			"Cannot be type of ExternalName for the Tenant %s", tnt.GetName(),
		)

		response := admission.Denied(NewExternalNameDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.LoadBalancer {
		recorder.Eventf(
			tnt,
			svc,
			corev1.EventTypeWarning,
			evt.ReasonForbiddenLoadBalancer,
			evt.ActionValidationDenied,
			"Cannot be type of LoadBalancer for the Tenant %s", tnt.GetName(),
		)

		response := admission.Denied(NewLoadBalancerDisabled().Error())

		return &response
	}

	if tnt.Spec.ServiceOptions != nil {
		err := api.ValidateForbidden(svc.Annotations, tnt.Spec.ServiceOptions.ForbiddenAnnotations)
		if err != nil {
			err = errors.Wrap(err, "annotations validation failed")

			recorder.Eventf(
				svc,
				tnt,
				corev1.EventTypeWarning,
				evt.ReasonForbiddenAnnotation,
				evt.ActionValidationDenied,
				err.Error(),
			)

			response := admission.Denied(err.Error())

			return &response
		}

		err = api.ValidateForbidden(svc.Labels, tnt.Spec.ServiceOptions.ForbiddenLabels)
		if err != nil {
			err = errors.Wrap(err, "labels validation failed")

			recorder.Eventf(
				svc,
				tnt,
				corev1.EventTypeWarning,
				evt.ReasonForbiddenLabel,
				evt.ActionValidationDenied,
				err.Error(),
			)

			response := admission.Denied(err.Error())

			return &response
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
				evt.ReasonForbiddenExternalServiceIP,
				evt.ActionValidationDenied,
				"External IP %s is forbidden for the Tenant %s", ip.String(), tnt.GetName(),
			)

			response := admission.Denied(NewExternalServiceIPForbidden(tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed).Error())

			return &response
		}
	}

	return nil
}
