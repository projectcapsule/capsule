// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type requiredMetadataHandler struct{}

func RequiredMetadataHandler() handlers.TypedHandlerWithTenant[*corev1.Namespace] {
	return &requiredMetadataHandler{}
}

func (h *requiredMetadataHandler) OnCreate(
	_ client.Client,
	ns *corev1.Namespace,
	_ admission.Decoder,
	_ events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		no := tnt.Spec.NamespaceOptions
		if no == nil || no.RequiredMetadata == nil {
			return nil
		}

		rm := no.RequiredMetadata

		if resp := validateRequiredMapCreate(
			"label",
			rm.Labels,
			ns.GetLabels(),
		); resp != nil {
			return resp
		}

		if resp := validateRequiredMapCreate(
			"annotation",
			rm.Annotations,
			ns.GetAnnotations(),
		); resp != nil {
			return resp
		}

		return nil
	}
}

func (h *requiredMetadataHandler) OnUpdate(
	_ client.Client,
	newNs *corev1.Namespace,
	oldNs *corev1.Namespace,
	_ admission.Decoder,
	_ events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		no := tnt.Spec.NamespaceOptions
		if no == nil || no.RequiredMetadata == nil {
			return nil
		}

		rm := no.RequiredMetadata

		if resp := validateRequiredMapUpdate(
			"label",
			rm.Labels,
			newNs.GetLabels(),
			oldNs.GetLabels(),
		); resp != nil {
			return resp
		}

		if resp := validateRequiredMapUpdate(
			"annotation",
			rm.Annotations,
			newNs.GetAnnotations(),
			oldNs.GetAnnotations(),
		); resp != nil {
			return resp
		}

		return nil
	}
}

func (h *requiredMetadataHandler) OnDelete(
	client.Client,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func validateRequiredMapCreate(kind string, required map[string]string, actual map[string]string) *admission.Response {
	for key, exp := range required {
		val, ok := actual[key]
		if !ok {
			resp := admission.Denied(fmt.Sprintf("required %s %q not present", kind, key))

			return &resp
		}

		re, reErr := regexp.Compile(exp)
		if reErr != nil {
			resp := admission.Denied(fmt.Sprintf("invalid required %s regex for %q: %q: %v", kind, key, exp, reErr))

			return &resp
		}

		if !re.MatchString(val) {
			resp := admission.Denied(fmt.Sprintf("required %s %q value %q does not match regex %q", kind, key, val, exp))

			return &resp
		}
	}

	return nil
}

func validateRequiredMapUpdate(kind string, required map[string]string, newMap, oldMap map[string]string) *admission.Response {
	mismatchKind := kind
	if kind == "label" {
		mismatchKind = "annotation"
	}

	for key, exp := range required {
		valNew, newOK := newMap[key]
		valOld, oldOK := oldMap[key]

		if newOK == oldOK && (!newOK || valNew == valOld) {
			continue
		}

		if !newOK {
			resp := admission.Denied(fmt.Sprintf("required %s %q not present", kind, key))

			return &resp
		}

		re, reErr := regexp.Compile(exp)
		if reErr != nil {
			resp := admission.Denied(fmt.Sprintf("invalid required %s regex for %q: %q: %v", kind, key, exp, reErr))

			return &resp
		}

		if !re.MatchString(valNew) {
			resp := admission.Denied(fmt.Sprintf("required %s %q value %q does not match regex %q", mismatchKind, key, valNew, exp))

			return &resp
		}
	}

	return nil
}
