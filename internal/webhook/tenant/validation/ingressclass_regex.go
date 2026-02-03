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

type ingressClassRegexHandler struct{}

func IngressClassRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &ingressClassRegexHandler{}
}

func (h *ingressClassRegexHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.validate(tnt, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *ingressClassRegexHandler) OnDelete(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *ingressClassRegexHandler) OnUpdate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
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

func (h *ingressClassRegexHandler) validate(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	//nolint:staticcheck
	if tnt.Spec.IngressOptions.AllowedClasses != nil && len(tnt.Spec.IngressOptions.AllowedClasses.Regex) > 0 {
		if _, err := regexp.Compile(tnt.Spec.IngressOptions.AllowedClasses.Regex); err != nil {
			response := admission.Denied("unable to compile ingressClasses allowedRegex")

			return &response
		}
	}

	return nil
}
