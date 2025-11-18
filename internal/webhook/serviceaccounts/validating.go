// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type handler struct {
	cfg configuration.Configuration
}

func Handler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &handler{cfg: cfg}
}

func (r *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *handler) handle(ctx context.Context, clt client.Client, decoder admission.Decoder, req admission.Request, recorder record.EventRecorder) *admission.Response {
	sa := &corev1.ServiceAccount{}
	if err := decoder.Decode(req, sa); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.TenantByStatusNamespace(ctx, clt, sa.GetNamespace())
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	_, hasOwnerPromotion := sa.Labels[meta.OwnerPromotionLabel]
	if !hasOwnerPromotion {
		return nil
	}

	if !r.cfg.AllowServiceAccountPromotion() {
		response := admission.Denied(
			"service account owner promotion is disabled. Contact your system administrators",
		)

		return &response
	}

	// We don't want to allow promoted serviceaccounts to promote other serviceaccounts
	allowed, err := users.IsTenantOwner(ctx, clt, r.cfg, tnt, req.UserInfo)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if allowed {
		return nil
	}

	response := admission.Denied(
		"not permitted to promote serviceaccounts as owners",
	)

	return &response
}
