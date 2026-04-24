// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

func NamespaceHandler(configuration configuration.Configuration, hndlers ...handlers.TypedHandlerWithTenant[*corev1.Namespace]) handlers.Handler {
	return &handler{
		cfg:      configuration,
		handlers: hndlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []handlers.TypedHandlerWithTenant[*corev1.Namespace]
}

func (h *handler) OnCreate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := users.IsAdminUser(req, h.cfg.Administrators())

		if !userIsAdmin && !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		tnt, err := h.verifyReference(ctx, c, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		if terminating := h.rejectOnTermination(
			ctx,
			c,
			ns,
			tnt,
		); terminating != nil {
			return terminating
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, ns, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnDelete(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(c client.Client, decoder admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := users.IsAdminUser(req, h.cfg.Administrators())

		if !userIsAdmin && !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		oldTenant, err := h.verifyReference(ctx, c, oldNs)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if oldTenant == nil {
			return nil
		}

		newTenant, err := h.verifyReference(ctx, c, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if newTenant.GetName() != oldTenant.GetName() && newTenant.GetUID() != oldTenant.GetUID() {
			err := fmt.Errorf("namespace can not be migrated between tenants")

			return ad.ErroredResponse(err)
		}

		if terminating := h.rejectOnTermination(
			ctx,
			c,
			ns,
			newTenant,
		); terminating != nil {
			return terminating
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, ns, oldNs, decoder, recorder, oldTenant)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) verifyReference(
	ctx context.Context,
	c client.Client,
	ns *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	tenantByOwnerreference, err := tenant.GetTenantByOwnerreferences(ctx, c, ns.OwnerReferences)
	if err != nil {
		return nil, err
	}

	name := ""
	if tenantByOwnerreference != nil {
		name = tenantByOwnerreference.GetName()
	}

	if name != ns.Labels[meta.TenantLabel] {
		return nil, fmt.Errorf(
			"namespace label %q does not match owner reference %q",
			ns.Labels[meta.TenantLabel],
			name,
		)
	}

	return tenantByOwnerreference, nil
}

func (h *handler) rejectOnTermination(
	ctx context.Context,
	c client.Client,
	ns *corev1.Namespace,
	t *capsulev1beta2.Tenant,
) *admission.Response {
	tnt := &capsulev1beta2.Tenant{}

	_ = c.Get(ctx, types.NamespacedName{Name: t.GetName()}, tnt)

	if tnt.DeletionTimestamp == nil {
		return nil
	}

	instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
		Name: ns.GetName(),
		UID:  ns.GetUID(),
	})

	if instance != nil {
		return nil
	}

	err := fmt.Errorf("tenant is terminating and does not accept new namespaces")

	return ad.ErroredResponse(err)
}
