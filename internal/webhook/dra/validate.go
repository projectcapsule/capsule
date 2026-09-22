// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	corev1 "k8s.io/api/core/v1"
	resources "k8s.io/api/resource/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type deviceClass struct{}

func DeviceClass() handlers.Handler {
	return &deviceClass{}
}

func (h *deviceClass) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		switch res := req.Kind.Kind; res {
		case "ResourceClaim":
			rc := &resources.ResourceClaim{}
			if err := decoder.Decode(req, rc); err != nil {
				return ad.ErroredResponse(err)
			}

			return h.validateResourceRequest(ctx, c, decoder, recorder, req, rc.Namespace, rc.Spec.Devices.Requests, rc)
		case "ResourceClaimTemplate":
			rct := &resources.ResourceClaimTemplate{}
			if err := decoder.Decode(req, rct); err != nil {
				return ad.ErroredResponse(err)
			}

			return h.validateResourceRequest(ctx, c, decoder, recorder, req, rct.Namespace, rct.Spec.Spec.Devices.Requests, rct)
		default:
			return nil
		}
	}
}

func (h *deviceClass) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) OnUpdate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *deviceClass) validateResourceRequest(
	ctx context.Context,
	c client.Client,
	_ admission.Decoder,
	recorder events.EventRecorder,
	req admission.Request,
	namespace string,
	requests []resources.DeviceRequest,
	obj client.Object,
) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, c, namespace)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.DeviceClasses
	if allowed == nil {
		return nil
	}

	// Check every alternative: the scheduler may allocate any firstAvailable
	// subrequest, not just the first one listed by the tenant.
	classNames := make([]string, 0, len(requests))
	for _, dr := range requests {
		if dr.Exactly == nil && len(dr.FirstAvailable) == 0 {
			return ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
		}

		if dr.Exactly != nil {
			classNames = append(classNames, dr.Exactly.DeviceClassName)
		}

		for _, alternative := range dr.FirstAvailable {
			classNames = append(classNames, alternative.DeviceClassName)
		}
	}

	// An absent selector must not override an explicit name/regex allowlist.
	// Preserve the existing match-all behavior of a completely empty policy.
	matchSelector := len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 ||
		(len(allowed.Exact) == 0 && allowed.Regex == "")

	for i, name := range classNames {
		// Repeated references need one lookup per admission request. Do not cache
		// authorization across requests, where tenant policies or labels can change.
		if slices.Contains(classNames[:i], name) {
			continue
		}

		dc, err := utils.GetDeviceClassByName(ctx, c, name)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		if dc == nil {
			return ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
		}

		switch {
		case allowed.Match(dc.Name) || (matchSelector && allowed.SelectorMatch(dc)):
			continue
		default:
			recorder.LabeledEvent(
				obj,
				corev1.EventTypeWarning,
				events.ReasonForbiddenDeviceClass,
				events.ActionValidationDenied,
				fmt.Sprintf("%s %s/%s DeviceClass %s is forbidden for the current tenant", req.Kind.Kind, req.Namespace, req.Name, dc.Name),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(caperrors.NewDeviceClassForbidden(dc.Name, *allowed).Error())
		}
	}

	return nil
}
