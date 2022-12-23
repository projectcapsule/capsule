// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type saNameHandler struct{}

func ServiceAccountNameHandler() capsulewebhook.Handler {
	return &saNameHandler{}
}

func (h *saNameHandler) validateServiceAccountName(req admission.Request, decoder *admission.Decoder) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	compiler := regexp.MustCompile(`^.*:.*:.*(:.*)?$`)

	for _, owner := range tenant.Spec.Owners {
		if owner.Kind != "ServiceAccount" {
			continue
		}

		if !compiler.MatchString(owner.Name) {
			response := admission.Denied(fmt.Sprintf("owner name %s is not a valid Service Account name ", owner.Name))

			return &response
		}
	}

	return nil
}

func (h *saNameHandler) OnCreate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validateServiceAccountName(req, decoder)
	}
}

func (h *saNameHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *saNameHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validateServiceAccountName(req, decoder)
	}
}
