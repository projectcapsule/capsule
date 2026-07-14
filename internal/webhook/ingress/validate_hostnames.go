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
			recorder.LabeledEvent(
				ingress.GetClientObject(),
				corev1.EventTypeWarning,
				events.ReasonIngressHostnameEmpty,
				events.ActionValidationDenied,
				"ingress hostname is empty",
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.ErroredResponse(caperrors.NewEmptyIngressHostname(*tnt.Spec.IngressOptions.AllowedHostnames))
		}

		hostnameList.Insert(hostname)
	}

	if err = r.validateHostnames(*tnt, hostnameList); err == nil {
		return nil
	}

	var hostnameNotValidErr *caperrors.IngressHostnameNotValidError
	if errors.As(err, &hostnameNotValidErr) {
		recorder.LabeledEvent(
			ingress.GetClientObject(),
			corev1.EventTypeWarning,
			events.ReasonIngressHostnameNotValid,
			events.ActionValidationDenied,
			"ingress hostname is not valid",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny(err.Error())
	}

	return ad.ErroredResponse(err)
}

func (r *hostnames) validateHostnames(tenant capsulev1beta2.Tenant, hostnames sets.Set[string]) error {
	if tenant.Spec.IngressOptions.AllowedHostnames == nil {
		return nil
	}

	tenantHostnameSet := sets.New[string](tenant.Spec.IngressOptions.AllowedHostnames.Exact...)

	// Only hostnames outside the exact allow-list still need to be checked.
	notAllowedHostnames := hostnames.Difference(tenantHostnameSet).UnsortedList()

	//nolint:staticcheck
	if allowedRegex := tenant.Spec.IngressOptions.AllowedHostnames.Regex; len(allowedRegex) > 0 && len(notAllowedHostnames) > 0 {
		var failedRegexHostnames []string

		// compile regex once. if compilation fails, the remaining hostnames are not allowed
		re, err := regexp.Compile(allowedRegex)
		if err != nil {
			return caperrors.NewIngressHostnamesNotValid(notAllowedHostnames, *tenant.Spec.IngressOptions.AllowedHostnames)
		}

		for _, currentHostname := range notAllowedHostnames {
			if ok := re.MatchString(currentHostname); !ok {
				failedRegexHostnames = append(failedRegexHostnames, currentHostname)
			}
		}

		notAllowedHostnames = failedRegexHostnames
	}

	if len(notAllowedHostnames) > 0 {
		return caperrors.NewIngressHostnamesNotValid(notAllowedHostnames, *tenant.Spec.IngressOptions.AllowedHostnames)
	}

	return nil
}
