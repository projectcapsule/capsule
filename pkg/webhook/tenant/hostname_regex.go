// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package tenant

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type hostnameRegexHandler struct{}

func HostnameRegexHandler() capsulewebhook.Handler {
	return &hostnameRegexHandler{}
}

func (h *hostnameRegexHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.validate(decoder, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *hostnameRegexHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *hostnameRegexHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := h.validate(decoder, req); err != nil {
			return err
		}

		return nil
	}
}

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
