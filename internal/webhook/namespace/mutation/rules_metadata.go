// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	rulesmutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

type rulesMetadataMutation struct{ configuration configuration.Configuration }

func RulesMetadataHandler(cfg configuration.Configuration) handlers.TypedHandlerWithUser[*corev1.Namespace] {
	return &rulesMetadataMutation{configuration: cfg}
}

func (h *rulesMetadataMutation) OnCreate(c client.Client, reader client.Reader, _ users.AdmissionUser, ns *corev1.Namespace, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return mutateNamespaceRules(c, reader, h.configuration, ns)
}

func (h *rulesMetadataMutation) OnUpdate(c client.Client, reader client.Reader, _ users.AdmissionUser, _ *corev1.Namespace, ns *corev1.Namespace, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return mutateNamespaceRules(c, reader, h.configuration, ns)
}

func (*rulesMetadataMutation) OnDelete(client.Client, client.Reader, users.AdmissionUser, *corev1.Namespace, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func mutateNamespaceRules(c client.Client, reader client.Reader, cfg configuration.Configuration, ns *corev1.Namespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := tenant.GetTenantByLabels(ctx, reader, ns)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		bodies, err := tenant.BuildNamespaceRuleBodyStatus(c.Scheme(), ns, tnt)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		bodies, err = ruleengine.FilterNamespaceRulesByAudience(cfg, tnt, req, bodies)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		rulesmutation.MutateMetadata(ns, schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, bodies)

		return nil
	}
}
