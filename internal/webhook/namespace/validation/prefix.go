// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type prefixHandler struct {
	cfg configuration.Configuration
}

func PrefixHandler(configuration configuration.Configuration) handlers.TypedHandlerWithTenantUser[*corev1.Namespace] {
	return &prefixHandler{
		cfg: configuration,
	}
}

func (h *prefixHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	_ users.AdmissionUser,
	ns *corev1.Namespace,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if exp, _ := h.cfg.ProtectedNamespaceRegexp(); exp != nil {
			if exp.MatchString(ns.GetName()) {
				return ad.Denyf(
					"Creating namespaces with name matching %s regexp is not allowed; please, reach out to the system administrators",
					exp.String(),
				)
			}
		}

		enforcePrefix := h.cfg.ForceTenantPrefix()
		if tnt.Spec.ForceTenantPrefix != nil {
			enforcePrefix = *tnt.Spec.ForceTenantPrefix
		}

		if !enforcePrefix {
			return nil
		}

		expectedPrefix := tnt.GetName() + "-"
		if !strings.HasPrefix(ns.GetName(), expectedPrefix) {
			recorder.Eventf(
				ns,
				nil,
				corev1.EventTypeWarning,
				events.ReasonInvalidTenantPrefix,
				events.ActionValidationDenied,
				"Namespace %s does not match the expected prefix for the current Tenant",
				ns.GetName(),
			)

			return ad.Denyf(
				"The namespace doesn't match the tenant prefix, expected prefix %q",
				expectedPrefix,
			)
		}

		return nil
	}
}

func (h *prefixHandler) OnUpdate(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *prefixHandler) OnDelete(
	client.Client,
	client.Reader,
	users.AdmissionUser,
	*corev1.Namespace,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
