// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type cordoningHandler struct {
	cfg configuration.Configuration
}

func CordoningHandler(configuration configuration.Configuration) handlers.TypedHandlerWithTenantUser[*corev1.Namespace] {
	return &cordoningHandler{cfg: configuration}
}

func (h *cordoningHandler) OnCreate(
	c client.Client,
	_ client.Reader,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, c, user, ns, recorder, tnt)
	}
}

func (h *cordoningHandler) OnDelete(
	c client.Client,
	_ client.Reader,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, c, user, ns, recorder, tnt)
	}
}

func (h *cordoningHandler) OnUpdate(
	c client.Client,
	_ client.Reader,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, c, user, ns, recorder, tnt)
	}
}

func (h *cordoningHandler) validate(
	ctx context.Context,
	req admission.Request,
	c client.Client,
	user users.AdmissionUser,
	ns *corev1.Namespace,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if tnt.Spec.Cordoned && user.IsCapsule() {
		recorder.LabeledEvent(
			ns,
			corev1.EventTypeWarning,
			events.ReasonCordoning,
			events.ActionValidationDenied,
			"namespace can not be modified because the tenant is cordoned",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)

		return ad.Deny("the selected tenant is cordoned")
	}

	return nil
}
