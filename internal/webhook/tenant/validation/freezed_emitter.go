// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type freezedEmitterHandler struct{}

func FreezedEmitter() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &freezedEmitterHandler{}
}

func (h *freezedEmitterHandler) OnCreate(client.Client, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *freezedEmitterHandler) OnDelete(client.Client, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *freezedEmitterHandler) OnUpdate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		switch {
		case !old.Spec.Cordoned && tnt.Spec.Cordoned:
			recorder.Eventf(tnt, tnt, corev1.EventTypeNormal, evt.ReasonCordoning, evt.ActionCordoned, "Tenant has been cordoned", "")
		case old.Spec.Cordoned && !tnt.Spec.Cordoned:
			recorder.Eventf(tnt, tnt, corev1.EventTypeNormal, evt.ReasonCordoning, evt.ActionUncordoned, "Tenant has been uncordoned", "")
		}

		return nil
	}
}
