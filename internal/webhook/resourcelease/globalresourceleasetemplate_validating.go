// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcelease

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
	"github.com/projectcapsule/capsule/pkg/template"
)

func GlobalResourceLeaseTemplateValidationHandler(log logr.Logger) handlers.Handler {
	return &globalResourceLeaseTemplateValidationHandler{
		log: log,
	}
}

type globalResourceLeaseTemplateValidationHandler struct {
	log logr.Logger
}

func (b *globalResourceLeaseTemplateValidationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for GlobalResourceLeaseTemplate upon creation", "name", req.Name)

		return validateGlobalResourceLeaseTemplate(decoder, req)
	}
}

func (b *globalResourceLeaseTemplateValidationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return nil
	}
}

func (b *globalResourceLeaseTemplateValidationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for GlobalResourceLeaseTemplate upon update", "name", req.Name)

		return validateGlobalResourceLeaseTemplate(decoder, req)
	}
}

func validateGlobalResourceLeaseTemplate(decoder admission.Decoder, req admission.Request) *admission.Response {
	brt := &capsulev1beta2.GlobalResourceLeaseTemplate{}
	if err := decoder.Decode(req, brt); err != nil {
		return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
	}

	return validateResourceLeaseTemplate(brt)
}

func validateResourceLeaseTemplate(brt capsulev1beta2.ResourceLeaseTemplateSource) *admission.Response {
	templateData := brt.TemplateData()

	if err := brt.ValidateApprovalConditions(); err != nil {
		return ad.Denyf("approval conditions are invalid: %v", err)
	}

	for i, approver := range templateData.Approvals.Approvers {
		if approver.Name == "" {
			return ad.Denyf("approvals.approvers[%d].name must not be empty", i)
		}
	}
	// Ensure the template's own defaults are consistent.
	if templateData.MaxDuration != nil && templateData.MaxDuration.Duration > 0 && templateData.DefaultDuration != nil &&
		templateData.DefaultDuration.Duration > templateData.MaxDuration.Duration {
		return ad.Denyf(
			"defaultDuration %s exceeds maxDuration %s",
			templateData.DefaultDuration.Duration,
			templateData.MaxDuration.Duration,
		)
	}

	if err := template.ValidateResourceTemplates(templateData.ParamSchema, templateData.Resources); err != nil {
		return ad.Denyf("invalid resources: %v", err)
	}

	return nil
}
