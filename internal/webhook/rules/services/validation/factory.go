// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	ruleengine "github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type serviceRuleSet[R any] = ruleengine.Set[R, *corev1.Service]

func evaluateServiceRules[R any](
	svc *corev1.Service,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	set serviceRuleSet[R],
) (*ruleengine.Evaluation, error) {
	if svc == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	return ruleengine.EvaluateEnforce(
		svc,
		enforceBodies,
		set,
	)
}

type serviceRuleValidator func(
	*corev1.Service,
	[]*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error)

type serviceRules struct {
	rules      []serviceRuleValidator
	regexCache *cache.RegexCache
}

func ServiceRules(
	regexCache *cache.RegexCache,
) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Service] {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	h := &serviceRules{
		regexCache: regexCache,
	}

	h.rules = []serviceRuleValidator{
		h.validateServiceTypes,
		h.validateLoadBalancers,
		h.validateExternalNames,
		h.validateNodePorts,
	}

	return h
}

func (h *serviceRules) OnCreate(
	_ client.Client,
	_ client.Reader,
	svc *corev1.Service,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validateServiceRules(ctx, req, svc, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *serviceRules) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Service,
	svc *corev1.Service,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validateServiceRules(ctx, req, svc, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *serviceRules) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Service,
	_ admission.Decoder,
	_ events.EventRecorder,
	_ *capsulev1beta2.Tenant,
	_ []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *serviceRules) validateServiceRules(
	ctx context.Context,
	req admission.Request,
	svc *corev1.Service,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) error {
	for _, evaluate := range h.rules {
		evaluation, err := evaluate(svc, enforceBodies)
		if err != nil {
			return err
		}

		if evaluation == nil {
			continue
		}

		for _, audit := range evaluation.Audits {
			recorder.LabeledEvent(
				svc,
				corev1.EventTypeNormal,
				events.ReasonNamespaceRuleAudit,
				events.ActionRuleAudit,
				audit.Message,
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)
		}

		if err := evaluation.BlockingError(); err != nil {
			var decisionErr *ruleengine.DecisionError
			if errors.As(err, &decisionErr) && decisionErr.Decision != nil {
				recorder.LabeledEvent(
					svc,
					corev1.EventTypeWarning,
					decisionErr.Decision.EventReason,
					events.ActionValidationDenied,
					decisionErr.Decision.Message,
				).
					WithRelated(tnt).
					WithTenantLabel(tnt).
					WithRequestAnnotations(req).
					Emit(ctx)
			}

			return err
		}
	}

	return nil
}
