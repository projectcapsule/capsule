// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type metaHandler struct{}

func MetaHandler() capsulewebhook.Handler {
	return &metaHandler{}
}

func (h *metaHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metaHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *metaHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		tenant := &capsulev1beta2.Tenant{}
		if err := decoder.Decode(req, tenant); err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant.Labels != nil {
			if tenant.Labels[capsuleapi.TenantNameLabel] != "" {
				if tenant.Labels[capsuleapi.TenantNameLabel] != tenant.Name {
					response := admission.Denied(fmt.Sprintf("tenant label '%s' is immutable", capsuleapi.TenantNameLabel))

					return &response
				}
			}
		}

		return nil
	}
}
