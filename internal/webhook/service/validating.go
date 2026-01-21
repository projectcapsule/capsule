// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
)

type validating struct{}

func Validating() capsulewebhook.TypedHandlerWithTenant[*corev1.Service] {
	return &validating{}
}

func (h *validating) OnCreate(
	c client.Client,
	svc *corev1.Service,
	decoder admission.Decoder,
	recorder record.EventRecorder,
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
	recorder record.EventRecorder,
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
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) handle(
	req admission.Request,
	recorder record.EventRecorder,
	svc *corev1.Service,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if svc.Spec.Type == corev1.ServiceTypeNodePort && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.NodePort {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenNodePort", "Service %s/%s cannot be type of NodePort for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(caperrors.NewNodePortDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.ExternalName {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenExternalName", "Service %s/%s cannot be type of ExternalName for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(caperrors.NewExternalNameDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.LoadBalancer {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenLoadBalancer", "Service %s/%s cannot be type of LoadBalancer for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(caperrors.NewLoadBalancerDisabled().Error())

		return &response
	}

	if tnt.Spec.ServiceOptions != nil {
		err := api.ValidateForbidden(svc.Annotations, tnt.Spec.ServiceOptions.ForbiddenAnnotations)
		if err != nil {
			err = errors.Wrap(err, "service annotations validation failed")
			recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenAnnotationReason, err.Error())
			response := admission.Denied(err.Error())

			return &response
		}

		err = api.ValidateForbidden(svc.Labels, tnt.Spec.ServiceOptions.ForbiddenLabels)
		if err != nil {
			err = errors.Wrap(err, "service labels validation failed")
			recorder.Eventf(tnt, corev1.EventTypeWarning, api.ForbiddenLabelReason, err.Error())
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
			recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenExternalServiceIP", "Service %s/%s external IP %s is forbidden for the current Tenant", req.Namespace, req.Name, ip.String())

			response := admission.Denied(caperrors.NewExternalServiceIPForbidden(tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed).Error())

			return &response
		}
	}

	return nil
}
