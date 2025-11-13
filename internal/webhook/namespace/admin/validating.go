// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/namespace/validation"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type adminValidatingHandler struct {
	cfg configuration.Configuration
}

func AdminValidatingHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &adminValidatingHandler{
		cfg: cfg,
	}
}

func (h *adminValidatingHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, req, decoder, recorder)
	}
}

func (h *adminValidatingHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *adminValidatingHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *adminValidatingHandler) handle(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) *admission.Response {
	ns := &corev1.Namespace{}
	if err := decoder.DecodeRaw(req.Object, ns); err != nil {
		return utils.ErroredResponse(err)
	}

	// We only care for administrator interactions if the tenant label
	tnt, _ := utils.GetNamespaceTenant(ctx, c, ns, req, h.cfg, nil)
	if tnt == nil {
		return nil
	}

	if res := validation.HandleIsFull(ctx, c, ns, tnt, recorder); res != nil {
		return res
	}

	if res := validation.HandlePrefix(ctx, c, h.cfg, ns, tnt, recorder); res != nil {
		return res
	}

	return nil
}
