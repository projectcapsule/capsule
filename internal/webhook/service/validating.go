// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type validating struct{}

func Validating() handlers.TypedHandlerWithTenantWithRuleset[*corev1.Service] {
	return &validating{}
}

func (h *validating) OnCreate(
	_ client.Client,
	_ client.Reader,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	_ []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, recorder, svc, tnt)
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
	_ []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, recorder, svc, tnt)
	}
}

func (h *validating) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Service,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	[]*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) handle(
	ctx context.Context,
	req admission.Request,
	recorder events.EventRecorder,
	svc *corev1.Service,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if svc.Spec.Type == corev1.ServiceTypeNodePort && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.NodePort {
		recorder.LabeledEvent(
			svc,
			corev1.EventTypeWarning,
			events.ReasonForbiddenNodePort,
			events.ActionValidationDenied,
			"Cannot be type of nodeport for the tenant",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(caperrors.NewNodePortDisabledError().Error())
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.ExternalName {
		recorder.LabeledEvent(
			svc,
			corev1.EventTypeWarning,
			events.ReasonForbiddenExternalName,
			events.ActionValidationDenied,
			"cannot be type of externalname for the tenant",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(caperrors.NewExternalNameDisabledError().Error())
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.LoadBalancer {
		recorder.LabeledEvent(
			svc,
			corev1.EventTypeWarning,
			events.ReasonForbiddenLoadBalancer,
			events.ActionValidationDenied,
			"cannot be type of loadbalancer for the tenant",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(caperrors.NewLoadBalancerDisabled().Error())
	}

	if tnt.Spec.ServiceOptions != nil {
		err := api.ValidateForbidden(svc.Annotations, tnt.Spec.ServiceOptions.ForbiddenAnnotations)
		if err != nil {
			err = errors.Wrap(err, "annotations validation failed")

			recorder.LabeledEvent(
				svc,
				corev1.EventTypeWarning,
				events.ReasonForbiddenAnnotation,
				events.ActionValidationDenied,
				err.Error(),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(err.Error())
		}

		err = api.ValidateForbidden(svc.Labels, tnt.Spec.ServiceOptions.ForbiddenLabels)
		if err != nil {
			err = errors.Wrap(err, "labels validation failed")

			recorder.LabeledEvent(
				svc,
				corev1.EventTypeWarning,
				events.ReasonForbiddenLabel,
				events.ActionValidationDenied,
				err.Error(),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

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
			recorder.LabeledEvent(
				svc,
				corev1.EventTypeWarning,
				events.ReasonForbiddenExternalServiceIP,
				events.ActionValidationDenied,
				fmt.Sprintf("external ip %s is forbidden for the tenant", ip.String()),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(caperrors.NewExternalServiceIPForbidden(tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed).Error())
		}
	}

	return nil
}
