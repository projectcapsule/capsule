// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package resourcepool

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
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
		return nil
	}
}
