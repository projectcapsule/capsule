// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

func InCapsuleGroupsOrAdministrator(configuration configuration.Configuration, handlers ...webhook.TypedHandler[*corev1.Namespace]) webhook.Handler {
	return &adminHandler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type adminHandler struct {
	cfg      configuration.Configuration
	handlers []webhook.TypedHandler[*corev1.Namespace]
}

//nolint:dupl
func (h *adminHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		userIsAdmin := false

		if !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			if !users.IsAdminUser(req, h.cfg.Administrators()) {
				return nil
			}

			userIsAdmin = true
		}

		ns := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.Object, ns); err != nil {
			return ErroredResponse(err)
		}

		tnt, err := tenant.GetTenantByLabels(ctx, c, ns)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil && userIsAdmin {
			return nil
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnCreate(c, ns, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:dupl
func (h *adminHandler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			if !users.IsAdminUser(req, h.cfg.Administrators()) {
				return nil
			}

			ns := &corev1.Namespace{}
			if err := decoder.DecodeRaw(req.Object, ns); err != nil {
				return ErroredResponse(err)
			}

			tnt, err := tenant.GetTenantByLabels(ctx, c, ns)
			if err != nil {
				return ErroredResponse(err)
			}

			if tnt == nil {
				return nil
			}
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnDelete(c, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

//nolint:dupl
func (h *adminHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, c, h.cfg, req.UserInfo.Username, req.UserInfo.Groups) {
			if !users.IsAdminUser(req, h.cfg.Administrators()) {
				return nil
			}

			ns := &corev1.Namespace{}
			if err := decoder.DecodeRaw(req.Object, ns); err != nil {
				return ErroredResponse(err)
			}

			tnt, err := tenant.GetTenantByLabels(ctx, c, ns)
			if err != nil {
				return ErroredResponse(err)
			}

			if tnt == nil {
				return nil
			}
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}
