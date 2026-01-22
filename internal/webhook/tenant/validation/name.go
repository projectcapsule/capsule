// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

type nameHandler struct{}

func NameHandler() capsulewebhook.Handler {
	return &nameHandler{}
}

func (h *nameHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		tenant := &capsulev1beta2.Tenant{}
		if err := decoder.Decode(req, tenant); err != nil {
			return utils.ErroredResponse(err)
		}

		matched, _ := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, tenant.GetName())
		if !matched {
			response := admission.Denied("tenant name has forbidden characters")

			return &response
		}

		return nil
	}
}

func (h *nameHandler) OnDelete(client.Client, admission.Decoder, events.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *nameHandler) OnUpdate(client.Client, admission.Decoder, events.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
