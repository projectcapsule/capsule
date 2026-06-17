// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"regexp"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type hostnames struct {
	configuration configuration.Configuration
}

func Hostnames(configuration configuration.Configuration) handlers.Handler {
	return &hostnames{configuration: configuration}
}

func (r *hostnames) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *hostnames) OnUpdate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *hostnames) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *hostnames) validate(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) *admission.Response {
	ingress, err := FromRequest(req, decoder)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	var tnt *capsulev1beta2.Tenant

	tnt, err = TenantFromIngress(ctx, c, ingress)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if tnt == nil || tnt.Spec.IngressOptions.AllowedHostnames == nil {
		return nil
	}

	hostnameList := sets.New[string]()

	for hostname := range ingress.HostnamePathsPairs() {
		if len(hostname) == 0 {
			recorder.Eventf(ingress.GetClientObject(), tnt, corev1.EventTypeWarning, events.ReasonIngressHostnameEmpty, events.ActionValidationDenied, "Ingress %s/%s hostname is empty", ingress.Namespace(), ingress.Name())

			return ad.ErroredResponse(caperrors.NewEmptyIngressHostname(*tnt.Spec.IngressOptions.AllowedHostnames))
		}

		hostnameList.Insert(hostname)
	}

	if err = r.validateHostnames(*tnt, hostnameList); err == nil {
		return nil
	}

	var hostnameNotValidErr *caperrors.IngressHostnameNotValidError
	if errors.As(err, &hostnameNotValidErr) {
		recorder.Eventf(ingress.GetClientObject(), tnt, corev1.EventTypeWarning, events.ReasonIngressHostnameNotValid, events.ActionValidationDenied, "Ingress %s/%s hostname is not valid", ingress.Namespace(), ingress.Name())

		return ad.Deny(err.Error())
	}

	return ad.ErroredResponse(err)
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

	//nolint:staticcheck
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
		return caperrors.NewIngressHostnamesNotValid(invalidHostnames, notMatchingHostnames, *tenant.Spec.IngressOptions.AllowedHostnames)
	}

	return nil
}
