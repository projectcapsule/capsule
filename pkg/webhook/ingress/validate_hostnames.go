// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"regexp"

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

type hostnames struct {
	configuration configuration.Configuration
}

func Hostnames(configuration configuration.Configuration) capsulewebhook.Handler {
	return &hostnames{configuration: configuration}
}

func (r *hostnames) OnCreate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, c, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		if err = r.validateHostnames(*tenant, ingress.Hostnames()); err == nil {
			return nil
		}

		var hostnameNotValidErr *ingressHostnameNotValid

		if errors.As(err, &hostnameNotValidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameNotValid", "Ingress %s/%s hostname is not valid", ingress.Namespace(), ingress.Name())

			response := admission.Denied(err.Error())

			return &response
		}

		return utils.ErroredResponse(err)
	}
}

func (r *hostnames) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, c, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		err = r.validateHostnames(*tenant, ingress.Hostnames())

		var hostnameNotValidErr *ingressHostnameNotValid

		if errors.As(err, &hostnameNotValidErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameNotValid", "Ingress %s/%s hostname is not valid", ingress.Namespace(), ingress.Name())

			response := admission.Denied(err.Error())

			return &response
		}

		return utils.ErroredResponse(err)
	}
}

func (r *hostnames) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *hostnames) validateHostnames(tenant capsulev1beta1.Tenant, hostnames []string) error {
	if tenant.Spec.IngressOptions == nil || tenant.Spec.IngressOptions.AllowedHostnames == nil {
		return nil
	}

	var valid, matched bool

	var invalidHostnames []string
	if len(hostnames) > 0 {
		for _, currentHostname := range hostnames {
			isPresent := HostnamesList(tenant.Spec.IngressOptions.AllowedHostnames.Exact).IsStringInList(currentHostname)
			if !isPresent {
				invalidHostnames = append(invalidHostnames, currentHostname)
			}
		}
		if len(invalidHostnames) == 0 {
			valid = true
		}
	}

	var notMatchingHostnames []string
	allowedRegex := tenant.Spec.IngressOptions.AllowedHostnames.Regex
	if len(allowedRegex) > 0 {
		for _, currentHostname := range hostnames {
			matched, _ = regexp.MatchString(allowedRegex, currentHostname)
			if !matched {
				notMatchingHostnames = append(notMatchingHostnames, currentHostname)
			}
		}
		if len(notMatchingHostnames) == 0 {
			matched = true
		}
	}

	if !valid && !matched {
		return NewIngressHostnamesNotValid(invalidHostnames, notMatchingHostnames, *tenant.Spec.IngressOptions.AllowedHostnames)
	}

	return nil
}
