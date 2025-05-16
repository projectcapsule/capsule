// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package resourcepool

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/meta"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type freezeHandler struct {
	log logr.Logger
}

func FreezeHandler(log logr.Logger) capsulewebhook.Handler {
	return &freezeHandler{log: log}
}

func (h *freezeHandler) OnUpdate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, c)
	}
}

func (h *freezeHandler) OnDelete(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, c)
	}
}

func (h *freezeHandler) OnCreate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, c)
	}
}

func (h *freezeHandler) handle(
	ctx context.Context,
	req admission.Request,
	decoder admission.Decoder,
	c client.Client,
) *admission.Response {
	obj := &unstructured.Unstructured{}
	if err := decoder.Decode(req, obj); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode object: %w", err))
	}

	switch req.Operation {
	case admissionv1.Create, admissionv1.Delete:
		if !meta.FreezeLabelTriggers(obj) {
			return nil
		}

		response := admission.Denied("Resource is frozen and can't be changed")

		return &response
	case admissionv1.Update:
		return h.handleFreezeOnUpdate(req, decoder, obj)
	default:
		return nil
	}
}

func (h *freezeHandler) handleFreezeOnUpdate(
	req admission.Request,
	decoder admission.Decoder,
	newObj *unstructured.Unstructured,
) *admission.Response {
	oldObj := &unstructured.Unstructured{}
	if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode old object: %w", err))
	}

	if !meta.FreezeLabelTriggers(oldObj) {
		return nil
	}

	response := admission.Denied("Resource is frozen and can't be changed")

	return &response
}
