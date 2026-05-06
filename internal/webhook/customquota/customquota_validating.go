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
	"github.com/projectcapsule/capsule/internal/cache"
	controller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

type customQuotaValidationHandler struct {
	targetsCache  *cache.CompiledTargetsCache[string]
	jsonPathCache *cache.JSONPathCache
}

func CustomQuotaValidationHandler(
	targetsCache *cache.CompiledTargetsCache[string],
	jsonPathCache *cache.JSONPathCache,
) handlers.Handler {
	return &customQuotaValidationHandler{
		targetsCache:  targetsCache,
		jsonPathCache: jsonPathCache,
	}
}

//nolint:dupl
func (h *customQuotaValidationHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		q := &capsulev1beta2.CustomQuota{}

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

// Invalidate Cache.
func (h *customQuotaValidationHandler) OnDelete(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		obj := &capsulev1beta2.CustomQuota{}
		if err := decoder.DecodeRaw(req.OldObject, obj); err != nil {
			return ad.ErroredResponse(err)
		}

		key := controller.MakeCustomQuotaCacheKey(obj.GetNamespace(), obj.GetName())

		if h.targetsCache != nil {
			h.targetsCache.Delete(key)
		}

		h.jsonPathCache.DeleteMany(obj.Spec.CollectJSONPathExpressions()...)

		return nil
	}
}

//nolint:dupl
func (h *customQuotaValidationHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		oldQuota := &capsulev1beta2.CustomQuota{}
		newQuota := &capsulev1beta2.CustomQuota{}

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
		}

		return nil
	}
}
