// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/breaktheglass/conditions"
	"github.com/projectcapsule/capsule/internal/breaktheglass/template"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func BreakRequestTemplateValidationHandler(log logr.Logger) handlers.Handler {
	return &breakRequestTemplateValidationHandler{
		log: log,
	}
}

type breakRequestTemplateValidationHandler struct {
	log logr.Logger
}

func (b *breakRequestTemplateValidationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for BreakRequestTemplate upon update", "name", req.Name)

		return validate(decoder, req)
	}
}

func (b *breakRequestTemplateValidationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return nil
	}
}

func (b *breakRequestTemplateValidationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for BreakRequestTemplate upon update", "name", req.Name)

		return validate(decoder, req)
	}
}

func validate(decoder admission.Decoder, req admission.Request) *admission.Response {
	brt := &capsulev1beta2.BreakRequestTemplate{}
	if err := decoder.Decode(req, brt); err != nil {
		return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
	}

	if !brt.Spec.AutoApprove {
		if brt.Spec.ApprovalCondition != "" {
			return ad.Denyf("approvalCondition should not be set when autoApprove is false")
		}
	} else {
		if brt.Spec.ApprovalCondition == "" {
			return nil
		}

		if _, err := conditions.PrepareCondition(brt); err != nil {
			return ad.Denyf("approvalCondition is invalid: %v", err)
		}
	}

	if err := template.ValidateItems(brt.Spec.Items); err != nil {
		return ad.Denyf("error rendering template: %v", err)
	}

	return nil
}
