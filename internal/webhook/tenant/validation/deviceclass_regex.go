// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package validation

import (
	"context"
	"regexp"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type deviceClassRegexHandler struct{}

func DeviceClassRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &deviceClassRegexHandler{}
}

func (h *deviceClassRegexHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
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

func (h *deviceClassRegexHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClassRegexHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	_ *capsulev1beta2.Tenant,
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

func (h *deviceClassRegexHandler) validate(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	//nolint:staticcheck
	if tnt.Spec.DeviceClasses != nil && len(tnt.Spec.DeviceClasses.Regex) > 0 {
		if _, err := regexp.Compile(tnt.Spec.DeviceClasses.Regex); err != nil {
			return ad.Deny("unable to compile deviceClasses allowedRegex")
		}
	}

	return nil
}
