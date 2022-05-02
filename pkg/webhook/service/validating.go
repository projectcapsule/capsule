// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) handleService(ctx context.Context, clt client.Client, decoder *admission.Decoder, req admission.Request, recorder record.EventRecorder) *admission.Response {
	svc := &corev1.Service{}
	if err := decoder.Decode(req, svc); err != nil {
		return utils.ErroredResponse(err)
	}

	tntList := &capsulev1beta1.TenantList{}
	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", svc.GetNamespace()),
	}); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(tntList.Items) == 0 {
		return nil
	}

	tnt := tntList.Items[0]

	if svc.Spec.Type == corev1.ServiceTypeNodePort && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.NodePort {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenNodePort", "Service %s/%s cannot be type of NodePort for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(NewNodePortDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.ExternalName {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenExternalName", "Service %s/%s cannot be type of ExternalName for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(NewExternalNameDisabledError().Error())

		return &response
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && tnt.Spec.ServiceOptions != nil && tnt.Spec.ServiceOptions.AllowedServices != nil && !*tnt.Spec.ServiceOptions.AllowedServices.LoadBalancer {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenLoadBalancer", "Service %s/%s cannot be type of LoadBalancer for the current Tenant", req.Namespace, req.Name)

		response := admission.Denied(NewLoadBalancerDisabled().Error())

		return &response
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
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "ForbiddenExternalServiceIP", "Service %s/%s external IP %s is forbidden for the current Tenant", req.Namespace, req.Name, ip.String())

			response := admission.Denied(NewExternalServiceIPForbidden(tnt.Spec.ServiceOptions.ExternalServiceIPs.Allowed).Error())

			return &response
		}
	}

	return nil
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handleService(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handleService(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
