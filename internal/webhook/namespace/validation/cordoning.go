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
		if tnt.Spec.Cordoned && user.IsCapsule() {
			recorder.Eventf(ns, nil, corev1.EventTypeWarning, events.ReasonCordoning, events.ActionValidationDenied, "Namespace %s cannot be attached, the current Tenant is cordoned", ns.GetName())

			return ad.Deny("the selected Tenant is cordoned")
		}

		return nil
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
		if tnt.Spec.Cordoned && user.IsCapsule() {
			recorder.Eventf(ns, tnt, corev1.EventTypeWarning, "TenantFreezed", "Denied", "Namespace %s cannot be deleted, the current Tenant is cordoned", req.Name)

			return ad.Deny("the selected Tenant is cordoned")
		}

		return nil
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
		if tnt.Spec.Cordoned && user.IsCapsule() {
			recorder.Eventf(ns, tnt, corev1.EventTypeWarning, "TenantFreezed", "Denied", "Namespace %s cannot be updated, the current Tenant is cordoned", ns.GetName())

			return ad.Deny("the selected Tenant is cordoned")
		}

		return nil
	}
}
