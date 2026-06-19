// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	indexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
)

type wildcard struct{}

func Wildcard() handlers.Handler {
	return &wildcard{}
}

func (h *wildcard) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, req, recorder, decoder)
	}
}

func (h *wildcard) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *wildcard) OnUpdate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, c, req, recorder, decoder)
	}
}

func (h *wildcard) validate(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	recorder events.EventRecorder,
	decoder admission.Decoder,
) *admission.Response {
	tntList := &capsulev1beta2.TenantList{}

	if err := c.List(ctx, tntList, client.MatchingFields{indexer.NamespaceIndexerFieldName: req.Namespace}); err != nil {
		return ad.ErroredResponse(err)
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
			return ad.ErroredResponse(err)
		}
		// Loop over all the hosts present on the ingress.
		for host := range ingress.HostnamePathsPairs() {
			// Check if one of the host has wildcard.
			if strings.HasPrefix(host, "*") {
				recorder.LabeledEvent(
					ingress.GetClientObject(),
					corev1.EventTypeWarning,
					events.ReasonWildcardDenied,
					events.ActionValidationDenied,
					fmt.Sprintf("%s %s/%s cannot be %s", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation))),
				).
					WithRelated(&tnt).
					WithTenantLabel(&tnt).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Denyf("Wildcard denied for tenant %s", tnt.GetName())
			}
		}
	}

	return nil
}
