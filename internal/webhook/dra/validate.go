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
	resourcesv1beta2 "k8s.io/api/resource/v1beta2"
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
		var obj client.Object

		switch req.Kind.Kind {
		case "ResourceClaim":
			if req.Kind.Version == resourcesv1beta2.SchemeGroupVersion.Version {
				obj = &resourcesv1beta2.ResourceClaim{}
			} else {
				obj = &resources.ResourceClaim{}
			}
		case "ResourceClaimTemplate":
			if req.Kind.Version == resourcesv1beta2.SchemeGroupVersion.Version {
				obj = &resourcesv1beta2.ResourceClaimTemplate{}
			} else {
				obj = &resources.ResourceClaimTemplate{}
			}
		default:
			return nil
		}

		if err := decoder.Decode(req, obj); err != nil {
			return ad.ErroredResponse(err)
		}

		return h.validateResourceRequest(ctx, c, recorder, req, obj)
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
	recorder events.EventRecorder,
	req admission.Request,
	obj client.Object,
) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, c, obj.GetNamespace())
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
	var names [resources.DeviceRequestsMaxSize]string

	classNames := requestedDeviceClasses(obj, names[:0])

	// An absent selector must not override an explicit name/regex allowlist.
	// Preserve the existing match-all behavior of a completely empty policy.
	//nolint:staticcheck
	matchSelector := len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 || (len(allowed.Exact) == 0 && allowed.Regex == "")

	for i, name := range classNames {
		if name == "" {
			return ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
		}

		// Repeated references need one lookup per admission request. Do not cache
		// authorization across requests, where tenant policies or labels can change.
		if slices.Contains(classNames[:i], name) {
			continue
		}

		dc, err := utils.GetDeviceClassByName(ctx, c, name, req.Kind.Version)
		if err != nil && !k8serrors.IsNotFound(err) {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		if dc == nil {
			return ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
		}

		switch {
		case allowed.Match(dc.GetName()) || (matchSelector && allowed.SelectorMatch(dc)):
			continue
		default:
			recorder.LabeledEvent(
				obj,
				corev1.EventTypeWarning,
				events.ReasonForbiddenDeviceClass,
				events.ActionValidationDenied,
				fmt.Sprintf("%s %s/%s DeviceClass %s is forbidden for the current tenant", req.Kind.Kind, req.Namespace, req.Name, dc.GetName()),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(caperrors.NewDeviceClassForbidden(dc.GetName(), *allowed).Error())
		}
	}

	return nil
}

// requestedDeviceClasses preserves every exact request and fallback. An empty
// name represents a missing request form and is rejected for restricted tenants.
func requestedDeviceClasses(obj client.Object, names []string) []string {
	switch claim := obj.(type) {
	case *resources.ResourceClaim:
		return deviceClassesV1(claim.Spec.Devices.Requests, names)
	case *resources.ResourceClaimTemplate:
		return deviceClassesV1(claim.Spec.Spec.Devices.Requests, names)
	case *resourcesv1beta2.ResourceClaim:
		return deviceClassesV1Beta2(claim.Spec.Devices.Requests, names)
	case *resourcesv1beta2.ResourceClaimTemplate:
		return deviceClassesV1Beta2(claim.Spec.Spec.Devices.Requests, names)
	default:
		return append(names, "")
	}
}

func deviceClassesV1(requests []resources.DeviceRequest, names []string) []string {
	for _, request := range requests {
		if request.Exactly != nil {
			names = append(names, request.Exactly.DeviceClassName)
		} else if len(request.FirstAvailable) == 0 {
			names = append(names, "")
		}

		for _, alternative := range request.FirstAvailable {
			names = append(names, alternative.DeviceClassName)
		}
	}

	return names
}

func deviceClassesV1Beta2(requests []resourcesv1beta2.DeviceRequest, names []string) []string {
	for _, request := range requests {
		if request.Exactly != nil {
			names = append(names, request.Exactly.DeviceClassName)
		} else if len(request.FirstAvailable) == 0 {
			names = append(names, "")
		}

		for _, alternative := range request.FirstAvailable {
			names = append(names, alternative.DeviceClassName)
		}
	}

	return names
}
