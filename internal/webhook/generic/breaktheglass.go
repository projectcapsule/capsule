// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type breakTheGlassResourceHandler struct{}

func BreakTheGlassResourceHandler() handlers.Handler {
	return &breakTheGlassResourceHandler{}
}

func (h *breakTheGlassResourceHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return h.handle
}

func (h *breakTheGlassResourceHandler) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return h.handle
}

func (h *breakTheGlassResourceHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return h.handle
}

func (h *breakTheGlassResourceHandler) handle(
	_ context.Context,
	req admission.Request,
) *admission.Response {
	user := users.NewAdmissionUser(users.AdmissionUserUnknown, req.UserInfo)
	if user.IsControllerServiceAccount() {
		return nil
	}

	return ad.Deny("resources protected by a BreakRequest can only be changed by the Capsule controller")
}
