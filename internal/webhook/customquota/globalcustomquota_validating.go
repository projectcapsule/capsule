// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

type globalCustomQuotaValidationHandler struct{}

func GlobalCustomQuotaValidationHandler() handlers.Handler {
	return &globalCustomQuotaValidationHandler{}
}

func (h *globalCustomQuotaValidationHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		q := &capsulev1beta2.GlobalCustomQuota{}

		if err := decoder.Decode(req, q); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		if err := quota.ValidateQuantity(q.Spec.Limit); err != nil {
			response := admission.Denied(fmt.Sprintf("invalid spec.limit: %v", err))
			return &response
		}

		return nil
	}
}

func (h *globalCustomQuotaValidationHandler) OnDelete(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *globalCustomQuotaValidationHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		oldQuota := &capsulev1beta2.GlobalCustomQuota{}
		newQuota := &capsulev1beta2.GlobalCustomQuota{}

		if err := decoder.DecodeRaw(req.OldObject, oldQuota); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode old object: %w", err))
		}

		if err := decoder.Decode(req, newQuota); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		if err := quota.ValidateQuantity(newQuota.Spec.Limit); err != nil {
			response := admission.Denied(fmt.Sprintf("invalid spec.limit: %v", err))
			return &response
		}

		used := oldQuota.Status.Usage.Used

		// No recorded usage: allow normal mutation rules below.
		hasUsage := used.Sign() > 0

		if hasUsage {
			if sourcesChanged(oldQuota.Spec.Sources, newQuota.Spec.Sources) {
				response := admission.Denied(
					fmt.Sprintf("spec.sources cannot be changed while usage is recorded (usage: %s); create a new CustomQuota instead", used.String()),
				)

				return &response
			}

			if newQuota.Spec.Limit.Cmp(used) < 0 {
				response := admission.Denied(
					fmt.Sprintf(
						"spec.limit cannot be lowered below current usage (%s); requested limit: %s",
						used.String(),
						newQuota.Spec.Limit.String(),
					),
				)
				return &response
			}

			// Unsure
			//if !reflect.DeepEqual(oldQuota.Spec.ScopeSelectors, newQuota.Spec.ScopeSelectors) {
			//	response := admission.Denied(
			//		"spec.scopeSelectors cannot be changed while usage is recorded",
			//	)
			//	return &response
			//}
		}

		return nil
	}
}
