// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func ResourcePermitTemplateValidationHandler(log logr.Logger) handlers.Handler {
	return &resourcePermitTemplateValidationHandler{log: log}
}

type resourcePermitTemplateValidationHandler struct {
	log logr.Logger
}

func (b *resourcePermitTemplateValidationHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for ResourcePermitTemplate upon creation", "namespace", req.Namespace, "name", req.Name)

		return validateNamespacedResourcePermitTemplate(decoder, req)
	}
}

func (b *resourcePermitTemplateValidationHandler) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response { return nil }
}

func (b *resourcePermitTemplateValidationHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for ResourcePermitTemplate upon update", "namespace", req.Namespace, "name", req.Name)

		return validateNamespacedResourcePermitTemplate(decoder, req)
	}
}

func validateNamespacedResourcePermitTemplate(
	decoder admission.Decoder,
	req admission.Request,
) *admission.Response {
	brt := &capsulev1beta2.ResourcePermitTemplate{}
	if err := decoder.Decode(req, brt); err != nil {
		return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
	}

	return validateResourcePermitTemplate(brt)
}
