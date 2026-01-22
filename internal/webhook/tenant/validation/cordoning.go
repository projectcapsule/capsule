// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

type cordoningHandler struct {
	configuration configuration.Configuration
}

func CordoningHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &cordoningHandler{
		configuration: configuration,
	}
}

func (h *cordoningHandler) OnCreate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req, recorder)
	}
}

func (h *cordoningHandler) OnDelete(c client.Client, _ admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req, recorder)
	}
}

func (h *cordoningHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, c, req, recorder)
	}
}

func (h *cordoningHandler) cordonHandler(ctx context.Context, c client.Client, req admission.Request, recorder events.EventRecorder) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, c, req.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	if tnt.Spec.Cordoned && users.IsCapsuleUser(ctx, c, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
		recorder.Eventf(tnt, nil, corev1.EventTypeWarning, "TenantFreezed", "%s %s/%s cannot be %sd, current Tenant is freezed", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

		response := admission.Denied(fmt.Sprintf("tenant %s is freezed: please, reach out to the system administrator", tnt.GetName()))

		return &response
	}

	return nil
}
