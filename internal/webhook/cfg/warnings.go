// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type warningHandler struct{}

func WarningHandler() handlers.TypedHandler[*capsulev1beta2.CapsuleConfiguration] {
	return &warningHandler{}
}

func (h *warningHandler) OnCreate(
	_ client.Client,
	cfg *capsulev1beta2.CapsuleConfiguration,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *warningHandler) OnDelete(client.Client, *capsulev1beta2.CapsuleConfiguration, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *warningHandler) OnUpdate(
	_ client.Client,
	cfg *capsulev1beta2.CapsuleConfiguration,
	_ *capsulev1beta2.CapsuleConfiguration,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(cfg, req)
	}
}

func (h *warningHandler) handle(config *capsulev1beta2.CapsuleConfiguration, req admission.Request) *admission.Response {
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
