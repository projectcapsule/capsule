// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"regexp"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type requiredMetadataHandler struct{}

func RequiredMetadataHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &requiredMetadataHandler{}
}

func (h *requiredMetadataHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if response := h.validate(tnt, req); response != nil {
			return response
		}

		return nil
	}
}

func (h *requiredMetadataHandler) OnDelete(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *requiredMetadataHandler) OnUpdate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if !requiredMetadataChanged(old, tnt) {
			return nil
		}

		if err := h.validate(tnt, req); err != nil {
			return err
		}

		return nil
	}
}

func (h *requiredMetadataHandler) validate(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	no := tnt.Spec.NamespaceOptions
	if no == nil || no.RequiredMetadata == nil {
		return nil
	}

	for _, exp := range tnt.Spec.NamespaceOptions.RequiredMetadata.Labels {
		if _, err := regexp.Compile(exp); err != nil {
			response := admission.Denied("unable to compile required label")

			return &response
		}
	}

	for _, exp := range tnt.Spec.NamespaceOptions.RequiredMetadata.Annotations {
		if _, err := regexp.Compile(exp); err != nil {
			response := admission.Denied("unable to compile required annotation")

			return &response
		}
	}

	return nil
}

func requiredMetadataChanged(oldT, newT *capsulev1beta2.Tenant) bool {
	oldRM := getRequiredMetadata(oldT)
	newRM := getRequiredMetadata(newT)

	// Both nil => no change
	if oldRM == nil && newRM == nil {
		return false
	}
	// One nil => changed
	if (oldRM == nil) != (newRM == nil) {
		return true
	}

	// Compare only the relevant maps
	if !utils.MapEqual(oldRM.Labels, newRM.Labels) {
		return true
	}

	if !utils.MapEqual(oldRM.Annotations, newRM.Annotations) {
		return true
	}

	return false
}

func getRequiredMetadata(t *capsulev1beta2.Tenant) *capsulev1beta2.RequiredMetadata {
	// Adjust the return type to your actual struct type:
	// e.g. *capsulev1beta2.NamespaceRequiredMetadata or similar.
	if t == nil || t.Spec.NamespaceOptions == nil || t.Spec.NamespaceOptions.RequiredMetadata == nil {
		return nil
	}

	return t.Spec.NamespaceOptions.RequiredMetadata
}
