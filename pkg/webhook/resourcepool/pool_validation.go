// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package resourcepool

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type poolValidationHandler struct {
	log logr.Logger
}

func PoolValidationHandler(log logr.Logger) capsulewebhook.Handler {
	return &poolValidationHandler{log: log}
}

func (h *poolValidationHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *poolValidationHandler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *poolValidationHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldPool := &capsulev1beta2.ResourcePool{}
		if err := decoder.DecodeRaw(req.OldObject, oldPool); err != nil {
			return utils.ErroredResponse(err)
		}

		pool := &capsulev1beta2.ResourcePool{}
		if err := decoder.Decode(req, pool); err != nil {
			return utils.ErroredResponse(err)
		}

		// Verify if resource decrease is allowed or no
		if !equality.Semantic.DeepEqual(pool.Spec.Quota.Hard, oldPool.Spec.Quota.Hard) {
			for resourceName, qt := range oldPool.Status.Allocation.Claimed {
				allocation, exists := pool.Spec.Quota.Hard[resourceName]
				if !exists {
					response := admission.Denied(fmt.Sprintf("can not remove resource %s as it is still being allocated %s. Remove corresponding claims or keep the resources in the pool", resourceName, allocation))

					return &response
				}

				if qt.Cmp(allocation) < 0 {
					response := admission.Denied(fmt.Sprintf("can not remove resource %s as it is still being allocated %s. Remove corresponding claims or keep the resources in the pool", resourceName, allocation))

					return &response
				}
			}
		}

		return nil
	}
}
