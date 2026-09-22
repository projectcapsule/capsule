// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func GlobalResourcePermitTemplateValidationHandler(log logr.Logger, conditions celruntime.ResourceConditionCompiler) handlers.Handler {
	return &globalResourcePermitTemplateValidationHandler{
		log:        log,
		conditions: conditions,
	}
}

type globalResourcePermitTemplateValidationHandler struct {
	log        logr.Logger
	conditions celruntime.ResourceConditionCompiler
}

func (b *globalResourcePermitTemplateValidationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for GlobalResourcePermitTemplate upon creation", "name", req.Name)

		return validateGlobalResourcePermitTemplate(decoder, req, b.conditions)
	}
}

func (b *globalResourcePermitTemplateValidationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return nil
	}
}

func (b *globalResourcePermitTemplateValidationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for GlobalResourcePermitTemplate upon update", "name", req.Name)

		return validateGlobalResourcePermitTemplate(decoder, req, b.conditions)
	}
}

func validateGlobalResourcePermitTemplate(decoder admission.Decoder, req admission.Request, conditions celruntime.ResourceConditionCompiler) *admission.Response {
	brt := &capsulev1beta2.GlobalResourcePermitTemplate{}
	if err := decoder.Decode(req, brt); err != nil {
		return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
	}

	return validateResourcePermitTemplate(brt, conditions)
}

func validateResourcePermitTemplate(brt capsulev1beta2.ResourcePermitTemplateSource, conditions celruntime.ResourceConditionCompiler) *admission.Response {
	if err := capsulev1beta2.ValidateResourcePermitTemplate(brt); err != nil {
		return ad.Deny(err.Error())
	}

	for i, resource := range brt.TemplateData().Resources {
		if resource.Policy.Condition == "" {
			continue
		}

		if conditions == nil {
			return ad.ErroredResponse(fmt.Errorf("resource condition compiler is not configured"))
		}

		if _, err := conditions.GetOrCompileResourceCondition(resource.Policy.Condition, environment.NewExpressions); err != nil {
			return ad.Denyf("spec.resources[%d].policy.condition: %v", i, err)
		}
	}

	return nil
}
