// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	namespaceindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/namespace"
)

type remainingNamespaceHandler struct{}

func RemainingNamespaceHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &remainingNamespaceHandler{}
}

func (h *remainingNamespaceHandler) OnCreate(
	client.Client,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

// This happens when a tenant has not yet reconciled it's namespaces but is deleted
// and in the meantime a new namespace was created referencing the same tenant.
func (h *remainingNamespaceHandler) OnDelete(
	c client.Client,
	tnt *capsulev1beta2.Tenant,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		list := &corev1.NamespaceList{}

		err := c.List(ctx, list, client.MatchingFields{namespaceindex.OwnerReferenceIndex: tnt.GetName()})
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if len(list.Items) == 0 {
			return nil
		}

		for _, ns := range list.Items {
			instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
				Name: ns.GetName(),
				UID:  ns.GetUID(),
			})

			if instance == nil {
				response := admission.Denied("tenant has remaining namespace referencing it (" + ns.GetName() + ")")

				return &response
			}
		}

		return nil
	}
}

func (h *remainingNamespaceHandler) OnUpdate(client.Client, *capsulev1beta2.Tenant, *capsulev1beta2.Tenant, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
