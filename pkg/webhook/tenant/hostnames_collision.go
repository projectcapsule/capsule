// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type hostnamesCollisionHandler struct {
	configuration configuration.Configuration
}

func HostnamesCollisionHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &hostnamesCollisionHandler{configuration: configuration}
}

func (h *hostnamesCollisionHandler) validateTenant(ctx context.Context, req admission.Request, clt client.Client, decoder *admission.Decoder) *admission.Response {
	tenant := &capsulev1beta1.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if !h.configuration.AllowTenantIngressHostnamesCollision() && tenant.Spec.IngressOptions != nil && tenant.Spec.IngressOptions.IngressHostnames != nil && len(tenant.Spec.IngressOptions.IngressHostnames.Exact) > 0 {
		for _, h := range tenant.Spec.IngressOptions.IngressHostnames.Exact {
			tntList := &capsulev1beta1.TenantList{}
			if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.ingressHostnames", h),
			}); err != nil {
				response := admission.Errored(http.StatusInternalServerError, fmt.Errorf("cannot retrieve Tenant list using .spec.ingressHostnames field selector: %w", err))

				return &response
			}
			switch {
			case len(tntList.Items) == 1 && tntList.Items[0].GetName() == tenant.GetName():
				continue
			case len(tntList.Items) > 0:
				response := admission.Denied(fmt.Sprintf("the allowed hostname %s is already used by the Tenant %s, cannot proceed", h, tntList.Items[0].GetName()))

				return &response
			default:
				continue
			}
		}
	}

	return nil
}

func (h *hostnamesCollisionHandler) OnCreate(client client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if response := h.validateTenant(ctx, req, client, decoder); response != nil {
			return response
		}

		return nil
	}
}

func (h *hostnamesCollisionHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *hostnamesCollisionHandler) OnUpdate(client client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if response := h.validateTenant(ctx, req, client, decoder); response != nil {
			return response
		}

		return nil
	}
}
