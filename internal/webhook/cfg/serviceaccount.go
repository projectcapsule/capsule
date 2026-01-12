// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

type serviceAccountHandler struct{}

func ServiceAccountHandler() capsulewebhook.Handler {
	return &serviceAccountHandler{}
}

func (h *serviceAccountHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *serviceAccountHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *serviceAccountHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(decoder, req)
	}
}

func (h *serviceAccountHandler) handle(decoder admission.Decoder, req admission.Request) *admission.Response {
	config := &capsulev1beta2.CapsuleConfiguration{}
	if err := decoder.Decode(req, config); err != nil {
		return utils.ErroredResponse(err)
	}

	nameSet := config.Spec.Impersonation.GlobalDefaultServiceAccount != ""
	nsSet := config.Spec.Impersonation.GlobalDefaultServiceAccountNamespace != ""

	if nameSet != nsSet {
		response := admission.Denied("both globalDefaultServiceAccount and globalDefaultServiceAccountNamespace must be set together")

		return &response
	}

	return nil
}
