// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

type poolMutationHandler struct {
	log logr.Logger
}

func PoolMutationHandler(log logr.Logger) capsulewebhook.Handler {
	return &poolMutationHandler{log: log}
}

func (h *poolMutationHandler) OnCreate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(req, decoder)
	}
}

func (h *poolMutationHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *poolMutationHandler) OnUpdate(_ client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.handle(req, decoder)
	}
}

func (h *poolMutationHandler) handle(
	req admission.Request,
	decoder admission.Decoder,
) *admission.Response {
	pool := &capsulev1beta2.ResourcePool{}
	if err := decoder.Decode(req, pool); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode object: %w", err))
	}

	// Correctly set the defaults
	h.handleDefaults(pool)

	// Marshal Manifest
	marshaled, err := json.Marshal(pool)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}

// Handles the Default Property. This is done at admission, to prevent and reconcile loops
// from gitops engines when ignores are not correctly set.
func (h *poolMutationHandler) handleDefaults(
	pool *capsulev1beta2.ResourcePool,
) {
	if !*pool.Spec.Config.DefaultsAssignZero {
		return
	}

	if pool.Spec.Defaults == nil {
		pool.Spec.Defaults = corev1.ResourceList{}
	}

	defaults := pool.Spec.Defaults

	for resourceName := range pool.Spec.Quota.Hard {
		amount, exists := pool.Spec.Defaults[resourceName]
		if !exists {
			amount = resource.MustParse("0")
		}

		defaults[resourceName] = amount
	}

	pool.Spec.Defaults = defaults
}
