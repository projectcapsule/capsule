// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
)

type managedValidatingHandler struct{}

func ManagedValidatingHandler() capsulewebhook.Handler {
	return &managedValidatingHandler{}
}

func (h *managedValidatingHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *managedValidatingHandler) OnDelete(client client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *managedValidatingHandler) OnUpdate(client client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *managedValidatingHandler) handler(ctx context.Context, clt client.Client, req admission.Request, recorder record.EventRecorder) *admission.Response {
	tntList := &capsulev1beta2.TenantList{}

	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", req.Namespace)}); err != nil {
		return utils.ErroredResponse(err)
	}

	return nil

}
