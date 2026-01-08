// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
	"github.com/projectcapsule/capsule/pkg/utils/users"
)

func NamespaceHandler(configuration configuration.Configuration, handlers ...webhook.TypedHandler[*corev1.Namespace]) webhook.Handler {
	return &handler{
		cfg:      configuration,
		handlers: handlers,
	}
}

type handler struct {
	cfg      configuration.Configuration
	handlers []webhook.TypedHandler[*corev1.Namespace]
}

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

		tnt, err := tenant.GetTenantByLabels(ctx, c, ns)
		if err != nil {
			return utils.ErroredResponse(err)
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

func (h *handler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) webhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
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

		tnt, err := tenant.GetTenantByOwnerreferences(ctx, c, oldNs.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		//nolint:nestif
		if userIsAdmin {
			if tnt == nil {
				tnt, err = tenant.GetTenantByLabels(ctx, c, ns)
				if err != nil {
					return utils.ErroredResponse(err)
				}

				if tnt == nil {
					return nil
				}
			}
		} else {
			if owned := tenant.NamespaceIsOwned(ctx, c, h.cfg, oldNs, tnt, req.UserInfo); !owned {
				recorder.Eventf(oldNs, corev1.EventTypeWarning, "NamespacePatch", "Namespace %s can not be patched", oldNs.GetName())

				response := admission.Denied("Denied patch request for this namespace")

				return &response
			}
		}

		for _, hndl := range h.handlers {
			if response := hndl.OnUpdate(c, ns, oldNs, decoder, recorder)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}
