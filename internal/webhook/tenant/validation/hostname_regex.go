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
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type hostnameRegexHandler struct{}

func HostnameRegexHandler() handlers.Handler {
	return &hostnameRegexHandler{}
}

func (h *hostnameRegexHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.validate(decoder, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *hostnameRegexHandler) OnDelete(client.Client, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *hostnameRegexHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := h.validate(decoder, req); err != nil {
			return err
		}

		return nil
	}
}

//nolint:staticcheck
func (h *hostnameRegexHandler) validate(decoder admission.Decoder, req admission.Request) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant.Spec.IngressOptions.AllowedHostnames != nil && len(tenant.Spec.IngressOptions.AllowedHostnames.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressOptions.AllowedHostnames.Regex); err != nil {
			response := admission.Denied("unable to compile allowedHostnames allowedRegex")

			return &response
		}
	}

	return nil
}
