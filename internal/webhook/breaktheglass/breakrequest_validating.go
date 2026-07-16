// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func BreakRequestValidationHandler(log logr.Logger) handlers.Handler {
	return &breakRequestValidationHandler{
		log: log,
	}
}

type breakRequestValidationHandler struct {
	log logr.Logger
}

func (b *breakRequestValidationHandler) OnCreate(_ client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for BreakRequest upon creation", "name", req.Name)

		br := &capsulev1beta2.BreakRequest{}
		if err := decoder.Decode(req, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		brt := &capsulev1beta2.BreakRequestTemplate{}
		if err := reader.Get(ctx, client.ObjectKey{Name: br.Spec.TemplateName}, brt); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return ad.Denyf("template %s not found", br.Spec.TemplateName)
			}

			return ad.ErroredResponse(fmt.Errorf("error loading template %s: %w", br.Spec.TemplateName, err))
		}

		if brt.Spec.MaxDuration.Duration > 0 &&
			br.Spec.Duration != nil &&
			br.Spec.Duration.Duration > brt.Spec.MaxDuration.Duration {
			return ad.Denyf("requested duration %s exceeds template maxDuration %s",
				br.Spec.Duration.Duration, brt.Spec.MaxDuration.Duration)
		}

		if br.Spec.StartTime != nil &&
			!br.Spec.StartTime.After(time.Now()) {
			return ad.Denyf("start time %s must be in the future", br.Spec.StartTime.String())
		}

		if _, err := br.RenderItems(brt.Spec.ParamSchema, brt.Spec.Templates); err != nil {
			return ad.Denyf("invalid template rendering for %s: %v", br.Spec.TemplateName, err)
		}

		return nil
	}
}

func (b *breakRequestValidationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, _ admission.Request) *admission.Response {
		return nil
	}
}

func (b *breakRequestValidationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		oldBr := &capsulev1beta2.BreakRequest{}
		newBr := &capsulev1beta2.BreakRequest{}

		if err := decoder.DecodeRaw(req.OldObject, oldBr); err != nil {
			return ad.ErroredResponse(err)
		}

		if err := decoder.Decode(req, newBr); err != nil {
			return ad.ErroredResponse(err)
		}

		if oldBr.Spec.TemplateName != newBr.Spec.TemplateName {
			return ad.Denyf(
				"templateName cannot be changed. old: %s, new: %s",
				oldBr.Spec.TemplateName,
				newBr.Spec.TemplateName,
			)
		}

		return nil
	}
}
