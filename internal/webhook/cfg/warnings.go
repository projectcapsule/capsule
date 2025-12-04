// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

type warningHandler struct{}

func WarningHandler() capsulewebhook.Handler {
	return &warningHandler{}
}

func (h *warningHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *warningHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *warningHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *warningHandler) handle(decoder admission.Decoder, req admission.Request) *admission.Response {
	config := &capsulev1beta2.CapsuleConfiguration{}
	if err := decoder.Decode(req, config); err != nil {
		return utils.ErroredResponse(err)
	}

	response := &admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			UID:     req.UID,
			Allowed: true,
		},
	}

	//nolint:staticcheck
	if len(config.Spec.UserGroups) > 0 {
		response.Warnings = append(response.Warnings,
			"The field `userGroups` is deprecated and will be removed in a future release. Please migrate to the `users` field. See: https://projectcapsule.dev/docs/operating/setup/configuration/#users.",
		)
	}

	//nolint:staticcheck
	if len(config.Spec.UserNames) > 0 {
		response.Warnings = append(response.Warnings,
			"The field `userNames` is deprecated and will be removed in a future release. Please migrate to the `users` field. See: https://projectcapsule.dev/docs/operating/setup/configuration/#users.",
		)
	}

	return response
}
