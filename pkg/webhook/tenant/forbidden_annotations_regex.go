// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type forbiddenAnnotationsRegexHandler struct{}

func ForbiddenAnnotationsRegexHandler() capsulewebhook.Handler {
	return &forbiddenAnnotationsRegexHandler{}
}

func (h *forbiddenAnnotationsRegexHandler) validate(decoder *admission.Decoder, req admission.Request) *admission.Response {
	tenant := &capsulev1beta1.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant.Annotations == nil {
		return nil
	}

	annotationsToCheck := []string{
		capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation,
		capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation,
	}

	for _, annotation := range annotationsToCheck {
		if _, err := regexp.Compile(tenant.Annotations[annotation]); err != nil {
			response := admission.Denied("unable to compile " + annotation + " regex annotation")

			return &response
		}
	}

	return nil
}

func (h *forbiddenAnnotationsRegexHandler) OnCreate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if err := h.validate(decoder, req); err != nil {
			return err
		}

		return nil
	}
}

func (h *forbiddenAnnotationsRegexHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *forbiddenAnnotationsRegexHandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if response := h.validate(decoder, req); response != nil {
			return response
		}

		return nil
	}
}
