// Copyright 2020-2025 Project Capsule Authors
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
		return h.validate(ctx, c, decoder, recorder, req)
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

func (h *deviceClass) validate(ctx context.Context, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, req admission.Request) *admission.Response {
	rc := &resources.ResourceClaim{}
	if err := decoder.Decode(req, rc); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.TenantByStatusNamespace(ctx, c, rc.Namespace)
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
	for _, dr := range rc.Spec.Devices.Requests {
		dc, err := utils.GetDeviceClassByName(ctx, c, dr.Exactly.DeviceClassName)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}
		if dc == nil {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "MissingDeviceClass", "ResourceClamim %s/%s is missing DeviceClass", req.Namespace, req.Name)

			response := admission.Denied(NewDeviceClassUndefined(*allowed).Error())

			return &response
		}
		selector := allowed.SelectorMatch(dc)
		switch {
		case allowed.MatchDefault(dc.Name):
			return nil
		case allowed.Match(dc.Name) || selector:
			return nil
		default:
			recorder.Eventf(tnt, corev1.EventTypeWarning, "ForbiddenDeviceClass", "ResourceClaim %s/%s DeviceClass %s is forbidden for the current Tenant", req.Namespace, req.Name, &dc)

			response := admission.Denied(NewDeviceClassForbidden(dc.Name, *allowed).Error())

			return &response
		}
	}
	return nil
}
