// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"

	resources "k8s.io/api/resource/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type deviceClass struct{}

func DeviceClass() capsulewebhook.Handler {
	return &deviceClass{}
}

func (h *deviceClass) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, decoder, recorder, req)
	}
}

func (h *deviceClass) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) validate(ctx context.Context, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, req admission.Request) *admission.Response {
	rct := &resources.ResourceClaim{}
	if err := decoder.Decode(req, rct); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.TenantByStatusNamespace(ctx, c, rct.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.DeviceClasses

	if allowed == nil {
		return nil
	}

	return nil
}
