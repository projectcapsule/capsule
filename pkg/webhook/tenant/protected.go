// Copyright 2020-2022 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type protectedHandler struct{}

func ProtectedHandler() capsulewebhook.Handler {
	return &protectedHandler{}
}

func (h *protectedHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *protectedHandler) OnDelete(clt client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tenant := &capsulev1beta1.Tenant{}

		if err := clt.Get(ctx, types.NamespacedName{Name: req.AdmissionRequest.Name}, tenant); err != nil {
			return utils.ErroredResponse(err)
		}

		if _, protected := tenant.Annotations[capsulev1beta1.ProtectedTenantAnnotation]; protected {
			response := admission.Denied(fmt.Sprintf("tenant is protected and cannot be deleted, remove %s annotation before proceeding", capsulev1beta1.ProtectedTenantAnnotation))

			return &response
		}

		return nil
	}
}

func (h *protectedHandler) OnUpdate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
