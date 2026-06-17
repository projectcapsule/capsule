// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type namespacePatchGuardHandler struct {
	cfg configuration.Configuration
}

func NamespacePatchGuardHandler(cfg configuration.Configuration) handlers.TypedHandlerWithUser[*corev1.Namespace] {
	return &namespacePatchGuardHandler{cfg: cfg}
}

func (h *namespacePatchGuardHandler) OnCreate(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespacePatchGuardHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespacePatchGuardHandler) OnUpdate(
	_ client.Client,
	reader client.Reader,
	user users.AdmissionUser,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if user.IsAdmin() {
			return nil
		}

		oldTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, newNs)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		switch {
		case oldTenant == nil && newTenant == nil:
			return denyNamespacePatch(oldNs, recorder, "namespace is not owned by any tenant")

		case oldTenant == nil && newTenant != nil:
			return denyNamespacePatch(oldNs, recorder, "namespace can not be patched into a tenant")

		case oldTenant != nil && newTenant == nil:
			return denyNamespacePatch(oldNs, recorder, "namespace can not remove tenant ownership")

		case oldTenant.GetName() != newTenant.GetName() || oldTenant.GetUID() != newTenant.GetUID():
			return denyNamespacePatch(oldNs, recorder, "namespace can not be migrated between tenants")
		}

		if !tenant.NamespaceIsOwned(ctx, reader, h.cfg, oldNs, oldTenant, user) {
			return denyNamespacePatch(oldNs, recorder, "denied patch request for this namespace")
		}

		return nil
	}
}

func denyNamespacePatch(
	ns *corev1.Namespace,
	recorder events.EventRecorder,
	message string,
) *admission.Response {
	if ns != nil {
		recorder.Eventf(
			ns,
			nil,
			corev1.EventTypeWarning,
			"NamespacePatch",
			events.ActionValidationDenied,
			"Namespace %s can not be patched: %s",
			ns.GetName(),
			message,
		)
	}

	return ad.Deny(message)
}
