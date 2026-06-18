// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

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

type podRuleSet[R any] = ruleengine.Set[R, *corev1.Pod]

func evaluatePodRules[R any](
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	set podRuleSet[R],
) (*ruleengine.Evaluation, error) {
	if pod == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	return ruleengine.EvaluateEnforce(
		pod,
		enforceBodies,
		set,
	)
}

type podRuleValidator func(
	*corev1.Pod,
	[]*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error)

type podRules struct {
	rules         []podRuleValidator
	regexCache    *cache.RegexCache
	registryCache *cache.RegistryRuleSetCache
}

func PodRules(
	regexCache *cache.RegexCache,
	registryCache *cache.RegistryRuleSetCache,
) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	if registryCache == nil {
		registryCache = cache.NewRegistryRuleSetCache(regexCache)
	}

	h := &podRules{
		regexCache:    regexCache,
		registryCache: registryCache,
	}

	h.rules = []podRuleValidator{
		h.validateSchedulers,
		h.validateQoSClasses,
		h.validateRegistries,
	}

	return h
}

func (h *podRules) OnCreate(
	_ client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, req, pod, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *podRules) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, req, pod, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *podRules) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Pod,
	_ admission.Decoder,
	_ events.EventRecorder,
	_ *capsulev1beta2.Tenant,
	_ []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *podRules) validatePodRules(
	ctx context.Context,
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) error {
	for _, evaluate := range h.rules {
		evaluation, err := evaluate(pod, enforceBodies)
		if err != nil {
			return err
		}

		if evaluation == nil {
			continue
		}

		// Audit is observational only. It must always be emitted when matched,
		// but it must never influence allow/deny decisions.
		for _, audit := range evaluation.Audits {
			recorder.LabeledEvent(
				pod,
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
					pod,
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
