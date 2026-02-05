// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"regexp"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

var compiler *regexp.Regexp = regexp.MustCompile(`^.*:.*:.*(:.*)?$`)

type saNameHandler struct{}

func ServiceAccountNameHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &saNameHandler{}
}

func (h *saNameHandler) OnCreate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validateServiceAccountName(tnt, req)
	}
}

func (h *saNameHandler) OnDelete(client.Client, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *saNameHandler) OnUpdate(
	_ client.Client,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validateServiceAccountName(tnt, req)
	}
}

func (h *saNameHandler) validateServiceAccountName(tnt *capsulev1beta2.Tenant, req admission.Request) *admission.Response {
	for _, owner := range tnt.Spec.Owners {
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
