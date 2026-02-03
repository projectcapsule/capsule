// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type wildcard struct{}

func Wildcard() handlers.Handler {
	return &wildcard{}
}

func (h *wildcard) OnCreate(client client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, client, req, recorder, decoder)
	}
}

func (h *wildcard) OnDelete(client.Client, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *wildcard) OnUpdate(client client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, client, req, recorder, decoder)
	}
}

func (h *wildcard) validate(ctx context.Context, clt client.Client, req admission.Request, recorder events.EventRecorder, decoder admission.Decoder) *admission.Response {
	tntList := &capsulev1beta2.TenantList{}

	if err := clt.List(ctx, tntList, client.MatchingFields{".status.namespaces": req.Namespace}); err != nil {
		return utils.ErroredResponse(err)
	}

	// resource is not inside a Tenant namespace
	if len(tntList.Items) == 0 {
		return nil
	}

	tnt := tntList.Items[0]

	if !tnt.Spec.IngressOptions.AllowWildcardHostnames {
		// Retrieve ingress resource from request.
		ingress, err := FromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}
		// Loop over all the hosts present on the ingress.
		for host := range ingress.HostnamePathsPairs() {
			// Check if one of the host has wildcard.
			if strings.HasPrefix(host, "*") {
				// In case of wildcard, generate an event and then return.
				recorder.Eventf(&tnt, nil, corev1.EventTypeWarning, evt.ReasonWildcardDenied, evt.ActionValidationDenied, "%s %s/%s cannot be %s", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

				response := admission.Denied(fmt.Sprintf("Wildcard denied for tenant %s\n", tnt.GetName()))

				return &response
			}
		}
	}

	return nil
}
