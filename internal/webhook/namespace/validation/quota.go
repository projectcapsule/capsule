// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
)

type quotaHandler struct{}

func QuotaHandler() capsulewebhook.TypedHandlerWithTenant[*corev1.Namespace] {
	return &quotaHandler{}
}

func (h *quotaHandler) OnCreate(
	c client.Client,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, recorder, ns, tnt)
	}
}

func (h *quotaHandler) OnDelete(
	client.Client,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *quotaHandler) OnUpdate(
	c client.Client,
	ns *corev1.Namespace,
	_ *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, recorder, ns, tnt)
	}
}

func (h *quotaHandler) handle(
	ctx context.Context,
	c client.Client,
	recorder events.EventRecorder,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) *admission.Response {
	if tnt.IsFull() {
		// Checking if the Namespace already exists.
		// If this is the case, no need to return the quota exceeded error:
		// the Kubernetes API Server will return an AlreadyExists error,
		// adhering more to the native Kubernetes experience.
		if err := c.Get(ctx, types.NamespacedName{Name: ns.Name}, &corev1.Namespace{}); err == nil {
			return nil
		}

		recorder.Eventf(tnt, ns, corev1.EventTypeWarning, "NamespaceQuotaExceded", "Namespace %s cannot be attached, quota exceeded for the current Tenant", ns.GetName())

		response := admission.Denied(NewNamespaceQuotaExceededError().Error())

		return &response
	}

	return nil
}
