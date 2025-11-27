// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

func NamespaceHandler(configuration configuration.Configuration, handlers ...webhook.TypedHandlerWithTenant[*corev1.Namespace]) webhook.Handler {
	return &handler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []webhook.TypedHandlerWithTenant[*corev1.Namespace]
}

//nolint:dupl
func (h *handler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := users.IsAdminUser(req, h.cfg.Administrators())

		if !userIsAdmin && !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := h.verifyReference(ctx, c, ns)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, ns, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:dupl
func (h *handler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := users.IsAdminUser(req, h.cfg.Administrators())

		if !userIsAdmin && !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := h.verifyReference(ctx, c, ns)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(c, ns, decoder, recorder, tnt)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *handler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := users.IsAdminUser(req, h.cfg.Administrators())

		if !userIsAdmin && !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return utils.ErroredResponse(err)
		}

		oldTenant, err := h.verifyReference(ctx, c, oldNs)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if oldTenant == nil {
			return nil
		}

		newTenant, err := h.verifyReference(ctx, c, ns)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if newTenant.GetName() != oldTenant.GetName() {
			err := fmt.Errorf("namespace can not be migrated between tenants")

			return utils.ErroredResponse(err)
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
