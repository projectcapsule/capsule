// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package validation

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type hostnameRegexHandler struct{}

func HostnameRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &hostnameRegexHandler{}
}

func (h *hostnameRegexHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.validate(tnt, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *hostnameRegexHandler) OnDelete(client.Client, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *hostnameRegexHandler) OnUpdate(
	_ client.Client,
	old *capsulev1beta2.Tenant,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := h.validate(tnt, req); err != nil {
			return err
		}

		return nil
	}
}

//nolint:staticcheck
func (h *hostnameRegexHandler) validate(
	tnt *capsulev1beta2.Tenant,
	req admission.Request,
) *admission.Response {
	if tnt.Spec.IngressOptions.AllowedHostnames != nil && len(tnt.Spec.IngressOptions.AllowedHostnames.Regex) > 0 {
		if _, err := regexp.Compile(tnt.Spec.IngressOptions.AllowedHostnames.Regex); err != nil {
			response := admission.Denied("unable to compile allowedHostnames allowedRegex")

			return &response
		}
	}

	return nil
}
