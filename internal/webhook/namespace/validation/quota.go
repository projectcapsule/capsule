// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type quotaHandler struct{}

func QuotaHandler() capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &quotaHandler{}
}

func (r *quotaHandler) OnCreate(client client.Client, ns *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(client, ns, recorder, ctx, req)
	}
}

func (r *quotaHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *quotaHandler) OnUpdate(client client.Client, ns *corev1.Namespace, _ *corev1.Namespace, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(client, ns, recorder, ctx, req)
	}
}

func (r *quotaHandler) handle(c client.Client, ns *corev1.Namespace, recorder record.EventRecorder, ctx context.Context, req admission.Request) *admission.Response {
	tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, ns.OwnerReferences)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	if tnt.IsFull() {
		// Checking if the Namespace already exists.
		// If this is the case, no need to return the quota exceeded error:
		// the Kubernetes API Server will return an AlreadyExists error,
		// adhering more to the native Kubernetes experience.
		if err := c.Get(ctx, types.NamespacedName{Name: ns.Name}, &corev1.Namespace{}); err == nil {
			return nil
		}

		recorder.Eventf(tnt, corev1.EventTypeWarning, "NamespaceQuotaExceded", "Namespace %s cannot be attached, quota exceeded for the current Tenant", ns.GetName())

		response := admission.Denied(NewNamespaceQuotaExceededError().Error())

		return &response
	}

	return nil
}
