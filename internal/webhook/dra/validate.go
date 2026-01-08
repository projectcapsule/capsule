// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	resources "k8s.io/api/resource/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type deviceClass struct{}

func DeviceClass() capsulewebhook.Handler {
	return &deviceClass{}
}

func (h *deviceClass) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		switch res := req.Kind.Kind; res {
		case "ResourceClaim":
			rc := &resources.ResourceClaim{}
			if err := decoder.Decode(req, rc); err != nil {
				return utils.ErroredResponse(err)
			}

			return h.validateResourceRequest(ctx, c, decoder, recorder, req, rc.Namespace, rc.Spec.Devices.Requests)
		case "ResourceClaimTemplate":
			rct := &resources.ResourceClaimTemplate{}
			if err := decoder.Decode(req, rct); err != nil {
				return utils.ErroredResponse(err)
			}

			return h.validateResourceRequest(ctx, c, decoder, recorder, req, rct.Namespace, rct.Spec.Spec.Devices.Requests)
		default:
			return nil
		}
	}
}

func (h *deviceClass) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) validateResourceRequest(ctx context.Context, c client.Client, _ admission.Decoder, recorder record.EventRecorder, req admission.Request, namespace string, requests []resources.DeviceRequest) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, c, namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.DeviceClasses
	if allowed == nil {
		return nil
	}

	for _, dr := range requests {
		dc, err := utils.GetDeviceClassByName(ctx, c, dr.Exactly.DeviceClassName)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		if dc == nil {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingDeviceClass", "%s %s/%s is missing DeviceClass", req.Kind.Kind, req.Namespace, req.Name)

			response := admission.Denied(NewDeviceClassUndefined(*allowed).Error())

			return &response
		}

		selector := allowed.SelectorMatch(dc)

		switch {
		case allowed.Match(dc.Name) || selector:
			return nil
		default:
			recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenDeviceClass", "%s %s/%s DeviceClass %s is forbidden for the current Tenant", req.Kind.Kind, req.Namespace, req.Name, &dc)

			response := admission.Denied(NewDeviceClassForbidden(dc.Name, *allowed).Error())

			return &response
		}
	}

	return nil
}
