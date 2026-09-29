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

type rulesMetadataMutation struct {
	configuration configuration.Configuration
	compiler      ruleengine.ConditionCompiler
}

func RulesMetadataHandler(cfg configuration.Configuration, compilers ...ruleengine.ConditionCompiler) handlers.TypedHandlerWithUser[*corev1.Namespace] {
	h := &rulesMetadataMutation{configuration: cfg}
	if len(compilers) > 0 {
		h.compiler = compilers[0]
	}

	return h
}

func (h *rulesMetadataMutation) OnCreate(c client.Client, reader client.Reader, _ users.AdmissionUser, ns *corev1.Namespace, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return mutateNamespaceRules(c, reader, h.configuration, ns, h.compiler)
}

func (h *rulesMetadataMutation) OnUpdate(c client.Client, reader client.Reader, _ users.AdmissionUser, ns *corev1.Namespace, _ *corev1.Namespace, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return mutateNamespaceRules(c, reader, h.configuration, ns, h.compiler)
}

func (*rulesMetadataMutation) OnDelete(client.Client, client.Reader, users.AdmissionUser, *corev1.Namespace, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func mutateNamespaceRules(c client.Client, reader client.Reader, cfg configuration.Configuration, ns *corev1.Namespace, compilers ...ruleengine.ConditionCompiler) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if req.SubResource == "finalize" {
			return nil
		}

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

		gvk := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
		if !rulesmutation.HasMetadataMutation(gvk, bodies) {
			return nil
		}

		bodies, err = ruleengine.FilterNamespaceRulesByAudience(ctx, c, cfg, tnt, req, bodies)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		var compiler ruleengine.ConditionCompiler
		if len(compilers) > 0 {
			compiler = compilers[0]
		}

		if _, err := rulesmutation.MutateMetadataConditional(ctx, ns, gvk, bodies, ruleengine.NewConditionEvaluator(compiler, req.AdmissionRequest)); err != nil {
			return handlers.ErroredResponse(err)
		}

		return nil
	}
}
