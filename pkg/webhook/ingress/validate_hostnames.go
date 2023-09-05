// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"regexp"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
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
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *hostnames) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *hostnames) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *hostnames) validate(ctx context.Context, client client.Client, req admission.Request, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
	ingress, err := FromRequest(req, decoder)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	var tenant *capsulev1beta2.Tenant

	tenant, err = TenantFromIngress(ctx, client, ingress)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant == nil || tenant.Spec.IngressOptions.AllowedHostnames == nil {
		return nil
	}

	hostnameList := sets.New[string]()

	for hostname := range ingress.HostnamePathsPairs() {
		if len(hostname) == 0 {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameEmpty", "Ingress %s/%s hostname is empty", ingress.Namespace(), ingress.Name())

			return utils.ErroredResponse(NewEmptyIngressHostname(*tenant.Spec.IngressOptions.AllowedHostnames))
		}

		hostnameList.Insert(hostname)
	}

	if err = r.validateHostnames(*tenant, hostnameList); err == nil {
		return nil
	}

	var hostnameNotValidErr *ingressHostnameNotValidError

	if errors.As(err, &hostnameNotValidErr) {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameNotValid", "Ingress %s/%s hostname is not valid", ingress.Namespace(), ingress.Name())

		response := admission.Denied(err.Error())

		return &response
	}

	return utils.ErroredResponse(err)
}

func (r *hostnames) validateHostnames(tenant capsulev1beta2.Tenant, hostnames sets.Set[string]) error {
	if tenant.Spec.IngressOptions.AllowedHostnames == nil {
		return nil
	}

	var valid, matched bool

	tenantHostnameSet := sets.New[string](tenant.Spec.IngressOptions.AllowedHostnames.Exact...)

	var invalidHostnames []string

	if len(hostnames) > 0 {
		if diff := hostnames.Difference(tenantHostnameSet); len(diff) > 0 {
			invalidHostnames = append(invalidHostnames, diff.UnsortedList()...)
		}

		if len(invalidHostnames) == 0 {
			valid = true
		}
	}

	var notMatchingHostnames []string

	if allowedRegex := tenant.Spec.IngressOptions.AllowedHostnames.Regex; len(allowedRegex) > 0 {
		for currentHostname := range hostnames {
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
