// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type freezedHandler struct {
	cfg configuration.Configuration
}

func FreezeHandler(configuration configuration.Configuration) capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &freezedHandler{cfg: configuration}
}

func (r *freezedHandler) OnCreate(
	c client.Client,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, ns.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		if tnt.Spec.Cordoned {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be attached, the current Tenant is freezed", ns.GetName())

			response := admission.Denied("the selected Tenant is freezed")

			return &response
		}

		return nil
	}
}

func (r *freezedHandler) OnDelete(
	c client.Client,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, ns.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		if tnt.Spec.Cordoned && users.IsCapsuleUser(ctx, c, r.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be deleted, the current Tenant is freezed", req.Name)

			response := admission.Denied("the selected Tenant is freezed")

			return &response
		}

		return nil
	}
}

func (r *freezedHandler) OnUpdate(
	c client.Client,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	decoder admission.Decoder,
	recorder record.EventRecorder,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, ns.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		if tnt.Spec.Cordoned && users.IsCapsuleUser(ctx, c, r.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be updated, the current Tenant is freezed", ns.GetName())

			response := admission.Denied("the selected Tenant is freezed")

			return &response
		}

		return nil
	}
}
