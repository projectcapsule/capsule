// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type patchHandler struct {
	cfg configuration.Configuration
}

func PatchHandler(configuration configuration.Configuration) capsulewebhook.TypedHandlerWithTenant[*corev1.Namespace] {
	return &patchHandler{cfg: configuration}
}

func (h *patchHandler) OnCreate(
	client.Client,
	*corev1.Namespace,
	admission.Decoder,
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *patchHandler) OnDelete(
	client.Client,
	*corev1.Namespace,
	admission.Decoder,
	record.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *patchHandler) OnUpdate(
	c client.Client,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		e := fmt.Sprintf("namespace/%s can not be patched", ns.Name)

		if ok := users.IsTenantOwnerByStatus(ctx, c, h.cfg, tnt, req.UserInfo); ok {
			return nil
		}

		recorder.Eventf(ns, corev1.EventTypeWarning, "NamespacePatch", e)
		response := admission.Denied(e)

		return &response
	}
}
