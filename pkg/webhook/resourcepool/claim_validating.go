// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepool

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type claimValidationHandler struct {
	log logr.Logger
}

func ClaimValidationHandler(log logr.Logger) capsulewebhook.Handler {
	return &claimValidationHandler{log: log}
}

func (h *claimValidationHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimValidationHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimValidationHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		oldClaim := &capsulev1beta2.ResourcePoolClaim{}
		newClaim := &capsulev1beta2.ResourcePoolClaim{}

		if err := decoder.DecodeRaw(req.OldObject, oldClaim); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode old object: %w", err))
		}

		if err := decoder.Decode(req, newClaim); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		if !reflect.DeepEqual(oldClaim.Spec.ResourceClaims, newClaim.Spec.ResourceClaims) {
			if oldClaim.IsBoundToResourcePool() {
				response := admission.Denied(fmt.Sprintf("cannot change the requested resources while claim is bound to a resourcepool %s", oldClaim.Status.Pool.Name))

				return &response
			}
		}

		if !reflect.DeepEqual(oldClaim.Spec.Pool, newClaim.Spec.Pool) {
			if oldClaim.IsBoundToResourcePool() {
				response := admission.Denied(fmt.Sprintf("cannot change the pool while claim is bound to a resourcepool %s", oldClaim.Status.Pool.Name))

				return &response
			}
		}

		return nil
	}
}
