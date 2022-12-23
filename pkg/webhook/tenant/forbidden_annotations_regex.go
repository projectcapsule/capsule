// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type forbiddenAnnotationsRegexHandler struct{}

func ForbiddenAnnotationsRegexHandler() capsulewebhook.Handler {
	return &forbiddenAnnotationsRegexHandler{}
}

func (h *forbiddenAnnotationsRegexHandler) validate(decoder *admission.Decoder, req admission.Request) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant.Spec.NamespaceOptions == nil {
		return nil
	}

	annotationsToCheck := map[string]string{
		"labels":      tenant.Spec.NamespaceOptions.ForbiddenLabels.Regex,
		"annotations": tenant.Spec.NamespaceOptions.ForbiddenAnnotations.Regex,
	}

	for scope, annotation := range annotationsToCheck {
		if _, err := regexp.Compile(tenant.Spec.NamespaceOptions.ForbiddenLabels.Regex); err != nil {
			response := admission.Denied(fmt.Sprintf("unable to compile %s regex for forbidden %s", annotation, scope))

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
