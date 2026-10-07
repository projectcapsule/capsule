// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type networkPolicyRules struct{ compiler ruleengine.ConditionCompiler }

// Handler joins the generic rules chain. Kind/subresource filtering happens
// before decoding and tenant/ruleset reads; a successful check continues it.
func Handler(cfg configuration.Configuration, compiler ruleengine.ConditionCompiler) handlers.Handler {
	return genericvalidation.ForKind(networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy").GroupKind(),
		&handlers.TypedTenantWithRulesetHandler[*networkingv1.NetworkPolicy]{
			Factory:       func() *networkingv1.NetworkPolicy { return &networkingv1.NetworkPolicy{} },
			Configuration: cfg,
			Handlers:      []handlers.TypedHandlerWithTenantWithRuleset[*networkingv1.NetworkPolicy]{&networkPolicyRules{compiler: compiler}},
		})
}

func (h *networkPolicyRules) OnCreate(_ client.Client, _ client.Reader, obj *networkingv1.NetworkPolicy, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(obj, recorder, tnt, bodies)
}

func (h *networkPolicyRules) OnUpdate(_ client.Client, _ client.Reader, _ *networkingv1.NetworkPolicy, obj *networkingv1.NetworkPolicy, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(obj, recorder, tnt, bodies)
}

func (*networkPolicyRules) OnDelete(client.Client, client.Reader, *networkingv1.NetworkPolicy, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *networkPolicyRules) validate(obj *networkingv1.NetworkPolicy, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforce, err := ruleengine.FilterEnforcementConditions(ctx,
			ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), obj,
			ruleengine.EnforceBodiesFromNamespaceRules(bodies), hasEgressCIDRs)
		if err != nil {
			return ad.Deny(fmt.Sprintf("enforce: %s", err))
		}

		result, err := evaluate(ctx, obj, enforce)
		if err != nil {
			return ad.Deny(err.Error())
		}

		if result == nil {
			return nil
		}

		blocking := result.BlockingError()

		if req.DryRun == nil || !*req.DryRun {
			for _, audit := range result.Audits {
				recorder.LabeledEvent(obj, corev1.EventTypeNormal, events.ReasonNamespaceRuleAudit, events.ActionRuleAudit, audit.Message).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
			}

			if blocking != nil {
				recorder.LabeledEvent(obj, corev1.EventTypeWarning, events.ReasonForbiddenNetworkPolicyEgressCIDR, events.ActionValidationDenied, blocking.Error()).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
			}
		}

		if blocking != nil {
			return ad.Deny(blocking.Error())
		}

		return nil
	}
}
