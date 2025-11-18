// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type patchHandler struct {
	cfg configuration.Configuration
}

func PatchHandler(configuration configuration.Configuration) capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &patchHandler{cfg: configuration}
}

func (r *patchHandler) OnCreate(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *patchHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *patchHandler) OnUpdate(
	c client.Client,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		e := fmt.Sprintf("namespace/%s can not be patched", ns.Name)

		if label, ok := ns.Labels[meta.TenantLabel]; ok {
			// retrieving the selected Tenant
			tnt := &capsulev1beta2.Tenant{}
			if err := c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
				response := admission.Errored(http.StatusBadRequest, err)

				return &response
			}

			ok, err := users.IsTenantOwner(ctx, c, r.cfg, tnt, req.UserInfo)
			if err != nil {
				response := admission.Errored(http.StatusBadRequest, err)

				return &response
			}

			if ok {
				return nil
			}
		}

		recorder.Eventf(ns, corev1.EventTypeWarning, "NamespacePatch", e)
		response := admission.Denied(e)

		return &response
	}
}
