// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

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

type forbiddenAnnotationsRegexHandler struct{}

func ForbiddenAnnotationsRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &forbiddenAnnotationsRegexHandler{}
}

func (h *forbiddenAnnotationsRegexHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
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
	client.Reader,
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
	_ client.Reader,
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

//nolint:staticcheck
func (h *forbiddenAnnotationsRegexHandler) validate(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	if tnt == nil {
		return nil
	}

	// Keep the paths explicit so admission identifies the field to repair.
	regexesToCheck := [4]struct{ path, expression string }{}
	if options := tnt.Spec.NamespaceOptions; options != nil {
		regexesToCheck[0].path = "spec.namespaceOptions.forbiddenLabels.deniedRegex"
		regexesToCheck[0].expression = options.ForbiddenLabels.Regex
		regexesToCheck[1].path = "spec.namespaceOptions.forbiddenAnnotations.deniedRegex"
		regexesToCheck[1].expression = options.ForbiddenAnnotations.Regex
	}

	if options := tnt.Spec.ServiceOptions; options != nil {
		regexesToCheck[2].path = "spec.serviceOptions.forbiddenLabels.deniedRegex"
		regexesToCheck[2].expression = options.ForbiddenLabels.Regex
		regexesToCheck[3].path = "spec.serviceOptions.forbiddenAnnotations.deniedRegex"
		regexesToCheck[3].expression = options.ForbiddenAnnotations.Regex
	}

	for _, check := range regexesToCheck {
		if check.expression == "" {
			continue
		}

		if _, err := regexp.Compile(check.expression); err != nil {
			return ad.Denyf(
				"unable to compile regex %q for %s: %v",
				check.expression,
				check.path,
				err,
			)
		}
	}

	return nil
}
