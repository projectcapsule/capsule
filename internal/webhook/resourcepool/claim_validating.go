// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepool

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type claimValidationHandler struct {
	log logr.Logger
}

func ClaimValidationHandler(log logr.Logger) handlers.Handler {
	return &claimValidationHandler{log: log}
}

func (h *claimValidationHandler) OnCreate(client.Client, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimValidationHandler) OnDelete(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		claim := &capsulev1beta2.ResourcePoolClaim{}

		if err := decoder.DecodeRaw(req.OldObject, claim); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode old object: %w", err))
		}

		if claim.IsBoundInResourcePool() {
			response := admission.Denied(fmt.Sprintf("cannot delete the pool while claim is used in resourcepool %s", claim.Status.Pool.Name))

			return &response
		}

		return nil
	}
}

func (h *claimValidationHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		oldClaim := &capsulev1beta2.ResourcePoolClaim{}
		newClaim := &capsulev1beta2.ResourcePoolClaim{}

		if err := decoder.DecodeRaw(req.OldObject, oldClaim); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode old object: %w", err))
		}

		if err := decoder.Decode(req, newClaim); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		if oldClaim.IsBoundInResourcePool() {
			if oldClaim.Spec.Pool != newClaim.Spec.Pool || !reflect.DeepEqual(oldClaim.Spec.ResourceClaims, newClaim.Spec.ResourceClaims) {
				response := admission.Denied(fmt.Sprintf("cannot change the requested resources while claim is allocated to a resourcepool %s", oldClaim.Status.Pool.Name))

				return &response
			}
		}

		return nil
	}
}
