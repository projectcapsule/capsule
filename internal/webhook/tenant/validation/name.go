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
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type nameHandler struct{}

func NameHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &nameHandler{}
}

func (h *nameHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		matched, _ := regexp.MatchString(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`, tnt.GetName())
		if !matched {
			response := admission.Denied("tenant name has forbidden characters")

			return &response
		}

		return nil
	}
}

func (h *nameHandler) OnDelete(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *nameHandler) OnUpdate(
	client.Client,
	*capsulev1beta2.Tenant,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
