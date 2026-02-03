// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type forbiddenAnnotationsRegexHandler struct{}

func ForbiddenAnnotationsRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &forbiddenAnnotationsRegexHandler{}
}

func (h *forbiddenAnnotationsRegexHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if err := h.validate(tnt, req); err != nil {
			return err
		}

		return nil
	}
}

func (h *forbiddenAnnotationsRegexHandler) OnDelete(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *forbiddenAnnotationsRegexHandler) OnUpdate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
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

func (h *forbiddenAnnotationsRegexHandler) validate(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	if tnt.Spec.NamespaceOptions == nil {
		return nil
	}

	annotationsToCheck := map[string]string{
		"labels":      tnt.Spec.NamespaceOptions.ForbiddenLabels.Regex,
		"annotations": tnt.Spec.NamespaceOptions.ForbiddenAnnotations.Regex,
	}

	for scope, annotation := range annotationsToCheck {
		if _, err := regexp.Compile(tnt.Spec.NamespaceOptions.ForbiddenLabels.Regex); err != nil {
			response := admission.Denied(fmt.Sprintf("unable to compile %s regex for forbidden %s", annotation, scope))

			return &response
		}
	}

	return nil
}
