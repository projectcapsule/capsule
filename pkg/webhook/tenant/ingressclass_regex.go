// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package tenant

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type ingressClassRegexHandler struct{}

func IngressClassRegexHandler() capsulewebhook.Handler {
	return &ingressClassRegexHandler{}
}

func (h *ingressClassRegexHandler) validate(decoder *admission.Decoder, req admission.Request) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant.Spec.IngressOptions.AllowedClasses != nil && len(tenant.Spec.IngressOptions.AllowedClasses.Regex) > 0 {
		if _, err := regexp.Compile(tenant.Spec.IngressOptions.AllowedClasses.Regex); err != nil {
			response := admission.Denied("unable to compile ingressClasses allowedRegex")

			return &response
		}
	}

	return nil
}

func (h *ingressClassRegexHandler) OnCreate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if response := h.validate(decoder, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *ingressClassRegexHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ingressClassRegexHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if err := h.validate(decoder, req); err != nil {
			return err
		}

		return nil
	}
}
