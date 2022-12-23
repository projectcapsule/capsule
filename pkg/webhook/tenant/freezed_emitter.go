// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type freezedEmitterHandler struct{}

func FreezedEmitter() capsulewebhook.Handler {
	return &freezedEmitterHandler{}
}

func (h *freezedEmitterHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *freezedEmitterHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *freezedEmitterHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldTnt := &capsulev1beta2.Tenant{}
		if err := decoder.DecodeRaw(req.OldObject, oldTnt); err != nil {
			return utils.ErroredResponse(err)
		}

		newTnt := &capsulev1beta2.Tenant{}
		if err := decoder.Decode(req, newTnt); err != nil {
			return utils.ErroredResponse(err)
		}

		switch {
		case !oldTnt.Spec.Cordoned && newTnt.Spec.Cordoned:
			recorder.Eventf(newTnt, corev1.EventTypeNormal, "TenantCordoned", "Tenant has been cordoned")
		case oldTnt.Spec.Cordoned && !newTnt.Spec.Cordoned:
			recorder.Eventf(newTnt, corev1.EventTypeNormal, "TenantUncordoned", "Tenant has been uncordoned")
		}

		return nil
	}
}
