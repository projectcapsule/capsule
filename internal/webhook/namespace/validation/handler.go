// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func NamespaceHandler(configuration configuration.Configuration, hndlers ...handlers.TypedHandlerWithTenantUser[*corev1.Namespace]) handlers.Handler {
	return &handler{
		cfg:      configuration,
		handlers: hndlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []handlers.TypedHandlerWithTenantUser[*corev1.Namespace]
}

func (h *handler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		if !user.IsAdmin() && !user.IsCapsule() && !tenant.HasTenantReference(ns) {
			return nil
		}

		tnt, err := tenant.ResolveNamespaceTenant(ctx, reader, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if !user.IsAdmin() && !user.IsCapsule() && tnt != nil {
			return ad.Deny("only tenant owners can create tenant-owned namespaces")
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
			if response := hndl.OnCreate(c, reader, user, ns, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		if !tenant.HasTenantReference(oldNs) {
			return nil
		}

		tnt, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(c, reader, user, oldNs, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:gocyclo,cyclop
func (h *handler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.cfg)

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return ad.ErroredResponse(err)
		}

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return ad.ErroredResponse(err)
		}

		oldHasTenantReference := tenant.HasTenantReference(oldNs)
		newHasTenantReference := tenant.HasTenantReference(ns)
		oldTenantLabel := tenant.TenanLabelValue(oldNs)
		newTenantLabel := tenant.TenanLabelValue(ns)

		if !oldHasTenantReference && !newHasTenantReference && oldTenantLabel != "" && newTenantLabel == "" {
			return nil
		}

		if !user.IsAdmin() {
			switch {
			case !oldHasTenantReference && newHasTenantReference:
				return ad.Deny("namespace can not be patched into a tenant")
			case oldHasTenantReference && !newHasTenantReference:
				return ad.Deny("namespace can not remove tenant ownership")
			case !oldHasTenantReference && !newHasTenantReference && oldTenantLabel == "" && newTenantLabel == "":
				return nil
			case !oldHasTenantReference && !newHasTenantReference:
				return ad.Deny("namespace can not be patched into a tenant")
			}
		}

		oldTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, oldNs)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		newTenant, err := tenant.ResolveNamespaceTenant(ctx, reader, ns)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if !user.IsAdmin() {
			if oldTenant == nil || newTenant == nil {
				return ad.Deny("namespace tenant ownership is incomplete")
			}

			if oldTenant.GetName() != newTenant.GetName() || oldTenant.GetUID() != newTenant.GetUID() {
				return ad.Deny("namespace can not be migrated between tenants")
			}

			if user.IsCapsule() && !tenant.NamespaceIsOwned(ctx, c, h.cfg, oldNs, oldTenant, user) {
				recorder.LabeledEvent(
					ns,
					corev1.EventTypeWarning,
					events.ReasonNamespaceHijack,
					events.ActionValidationDenied,
					"namespace can not be patched",
				).
					WithRequestAnnotations(req).
					Emit(ctx)

				return ad.Deny("denied patch request for this namespace")
			}
		}

		tnt := newTenant
		if !user.IsAdmin() {
			tnt = oldTenant
		}

		if tnt == nil {
			return nil
		}

		if terminating := h.rejectOnTermination(ctx, c, ns, newTenant); terminating != nil {
			return terminating
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, reader, user, ns, oldNs, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) rejectOnTermination(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
	t *capsulev1beta2.Tenant,
) *admission.Response {
	if t == nil {
		return nil
	}

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
