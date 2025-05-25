// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package resourcepool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type claimMutationHandler struct {
	log logr.Logger
}

func ClaimMutationHandler(log logr.Logger) capsulewebhook.Handler {
	return &claimMutationHandler{log: log}
}

func (h *claimMutationHandler) OnUpdate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, c)
	}
}

func (h *claimMutationHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimMutationHandler) OnCreate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, c)
	}
}

func (h *claimMutationHandler) handle(
	ctx context.Context,
	req admission.Request,
	decoder admission.Decoder,
	c client.Client,
) *admission.Response {
	claim := &capsulev1beta2.ResourcePoolClaim{}

	if err := decoder.Decode(req, claim); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
	}

	if err := h.autoAssignPools(ctx, c, claim); err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	h.handleReleaseAnnotation(claim)

	marshaled, err := json.Marshal(claim)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}

// Only Adds release label when necessary.
func (h *claimMutationHandler) handleReleaseAnnotation(
	claim *capsulev1beta2.ResourcePoolClaim,
) {
	if !meta.ReleaseAnnotationTriggers(claim) {
		return
	}

	if !claim.IsBoundToResourcePool() {
		return
	}

	meta.ReleaseAnnotationRemove(claim)
}

func (h *claimMutationHandler) autoAssignPools(
	ctx context.Context,
	c client.Client,
	claim *capsulev1beta2.ResourcePoolClaim,
) error {
	if claim.Spec.Pool != "" {
		return nil
	}

	poolList := &capsulev1beta2.ResourcePoolList{}
	if err := c.List(ctx, poolList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", claim.Namespace),
	}); err != nil {
		return err
	}

	if len(poolList.Items) == 0 {
		return nil
	}

	candidates := make([]*capsulev1beta2.ResourcePool, 0)

	for _, pool := range poolList.Items {
		assignable := true
		allocatable := true

		for resource, requested := range claim.Spec.ResourceClaims {
			if _, ok := pool.Status.Allocation.Hard[resource]; !ok {
				assignable = false

				break
			}

			available, ok := pool.Status.Allocation.Available[resource]
			if !ok || available.Cmp(requested) < 0 {
				allocatable = false

				break
			}
		}

		if !assignable {
			continue
		}

		if allocatable {
			candidates = append([]*capsulev1beta2.ResourcePool{&pool}, candidates...)

			continue
		}

		candidates = append(candidates, &pool)
	}

	if len(candidates) == 0 {
		return nil // no eligible pools
	}

	claim.Spec.Pool = candidates[0].Name

	return nil
}
