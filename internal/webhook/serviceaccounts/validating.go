// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type validating struct {
	cfg configuration.Configuration
}

func Validating(cfg configuration.Configuration) capsulewebhook.TypedHandlerWithTenant[*corev1.ServiceAccount] {
	return &validating{cfg: cfg}
}

func (h *validating) OnCreate(
	c client.Client,
	sa *corev1.ServiceAccount,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, req, sa, tnt)
	}
}

func (h *validating) OnUpdate(
	c client.Client,
	old *corev1.ServiceAccount,
	sa *corev1.ServiceAccount,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, req, sa, tnt)
	}
}

func (h *validating) OnDelete(
	client.Client,
	*corev1.ServiceAccount,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *validating) handle(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	sa *corev1.ServiceAccount,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	_, hasOwnerPromotion := sa.Labels[meta.OwnerPromotionLabel]
	if !hasOwnerPromotion {
		return nil
	}

	if !h.cfg.AllowServiceAccountPromotion() {
		response := admission.Denied(
			"service account owner promotion is disabled. Contact your system administrators",
		)

		return &response
	}

	// We don't want to allow promoted serviceaccounts to promote other serviceaccounts
	if ok := users.IsTenantOwnerByStatus(ctx, c, h.cfg, tnt, req.UserInfo); ok {
		return nil
	}

	response := admission.Denied(
		"not permitted to promote serviceaccounts as owners",
	)

	return &response
}
