// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package resourcepool

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
)

type claimMutationHandler struct {
	log logr.Logger
}

func ClaimMutationHandler(log logr.Logger) capsulewebhook.Handler {
	return &claimMutationHandler{log: log}
}

func (h *claimMutationHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimMutationHandler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *claimMutationHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

// Handles Pool-Assignment, It's done at webhook level to ensure that
// the resource pool is assigned before the claim is created. This should also help kubernetes-users
// to keep to workflow simple
func (h *claimMutationHandler) resourcePoolAssignment(
	ctx context.Context,
	c client.Client,
	claim *capsulev1beta2.ResourcePoolClaim,
) (pool *capsulev1beta2.ResourcePool, err error) {
	if claim.Spec.Pool != "" {
		poolList := &capsulev1beta2.ResourcePoolList{}
		if err := c.List(ctx, poolList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", claim.Namespace),
		}); err != nil {
			return nil, err
		}
	} else {
		poolList := &capsulev1beta2.ResourcePoolList{}
		if err := c.List(ctx, poolList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", claim.Namespace),
		}); err != nil {
			return nil, err
		}

		if len(poolList.Items) == 0 {
			return nil, fmt.Errorf("no resource pool found for namespace %s", claim.Namespace)
		}
		if len(poolList.Items) > 1 {
			return nil, fmt.Errorf("multiple resource pools found for namespace %s", claim.Namespace)
		}

		pool = &poolList.Items[0]
	}

	return
}
