// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type promotion struct {
	cfg configuration.Configuration
}

func Promotion(cfg configuration.Configuration) handlers.TypedHandlerWithTenantUser[*corev1.ServiceAccount] {
	return &promotion{cfg: cfg}
}

func (h *promotion) OnCreate(
	_ client.Client,
	_ client.Reader,
	user users.AdmissionUser,
	sa *corev1.ServiceAccount,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(user, recorder, sa, tnt)
	}
}

func (h *promotion) OnUpdate(
	_ client.Client,
	_ client.Reader,
	user users.AdmissionUser,
	old *corev1.ServiceAccount,
	sa *corev1.ServiceAccount,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(user, recorder, sa, tnt)
	}
}

func (h *promotion) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.ServiceAccount,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *promotion) handle(
	user users.AdmissionUser,
	recorder events.EventRecorder,
	sa *corev1.ServiceAccount,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	_, hasPromotion := sa.Labels[meta.ServiceAccountPromotionLabel]
	if !hasPromotion {
		return nil
	}

	if !h.cfg.AllowServiceAccountPromotion() {
		return ad.Deny("service account promotion is disabled. Contact cluster administrators")
	}

	// We don't want to allow promoted serviceaccounts to promote other serviceaccounts
	if ok := users.IsTenantOwnerByStatus(tnt, user); ok {
		return nil
	}

	msg := fmt.Sprintf("%s not allowed to promote serviceaccount to tenant owner", user.Username)

	recorder.Eventf(
		sa,
		tnt,
		corev1.EventTypeWarning,
		events.ReasonPromotionDenied,
		events.ActionValidationDenied,
		msg,
	)

	return ad.Deny(msg)
}
