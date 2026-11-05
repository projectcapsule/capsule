// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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

type podRuleValidator struct {
	evaluate func(
		*corev1.Pod,
		[]*apirules.NamespaceRuleEnforceBody,
	) (*ruleengine.Evaluation, error)
	// read replaces evaluate for validators that read a referenced object.
	read func(
		context.Context,
		client.Reader,
		*corev1.Pod,
		[]*apirules.NamespaceRuleEnforceBody,
	) (*ruleengine.Evaluation, error)
	includeSubresources bool
	// When set, only this subresource and the main resource are evaluated.
	subresource string
	changed     func(old, pod *corev1.Pod) bool
	// policy reports whether a rule carries this validator's policy. When set,
	// only a matching conditional rule that carries it checks an unchanged value
	// again; otherwise any conditional workload rule does.
	policy func(*apirules.NamespaceRuleEnforceBody) bool
}

// recheck reports whether an update that keeps the value must still be
// evaluated. Bodies are the rules whose conditions matched this request.
func (v *podRuleValidator) recheck(conditional bool, bodies []*apirules.NamespaceRuleEnforceBody) bool {
	if v.policy == nil {
		return conditional
	}

	return slices.ContainsFunc(bodies, func(body *apirules.NamespaceRuleEnforceBody) bool {
		return body != nil && len(body.Conditions) > 0 && v.policy(body)
	})
}

func (v *podRuleValidator) run(ctx context.Context, reader client.Reader, pod *corev1.Pod, bodies []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	if v.read != nil {
		return v.read(ctx, reader, pod, bodies)
	}

	return v.evaluate(pod, bodies)
}

type podRules struct {
	rules         []podRuleValidator
	regexCache    *cache.RegexCache
	registryCache *cache.RegistryRuleSetCache
	selectors     *cache.LabelSelectorCache
	compiler      ruleengine.ConditionCompiler
}

func PodRules(
	regexCache *cache.RegexCache,
	registryCache *cache.RegistryRuleSetCache,
	selectors *cache.LabelSelectorCache,
	compiler ruleengine.ConditionCompiler,
) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return newPodRules(regexCache, registryCache, selectors, compiler)
}

func newPodRules(regexCache *cache.RegexCache, registryCache *cache.RegistryRuleSetCache, selectors *cache.LabelSelectorCache, compiler ruleengine.ConditionCompiler) *podRules {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	if registryCache == nil {
		registryCache = cache.NewRegistryRuleSetCache(regexCache)
	}

	if selectors == nil {
		selectors = cache.NewLabelSelectorCache()
	}

	h := &podRules{
		regexCache:    regexCache,
		registryCache: registryCache,
		selectors:     selectors,
		compiler:      compiler,
	}

	h.rules = []podRuleValidator{
		{evaluate: h.validateSeccompProfiles, includeSubresources: true, subresource: "ephemeralcontainers"},
		{evaluate: h.validateAppArmorProfiles, includeSubresources: true, subresource: "ephemeralcontainers"},
		{evaluate: h.validateNodeSelectors, changed: func(old, pod *corev1.Pod) bool {
			return !equality.Semantic.DeepEqual(old.Spec.NodeSelector, pod.Spec.NodeSelector)
		}},
		{evaluate: h.validateTolerations, changed: func(old, pod *corev1.Pod) bool {
			return !equality.Semantic.DeepEqual(old.Spec.Tolerations, pod.Spec.Tolerations)
		}},
		{evaluate: h.validateTopologySpread, changed: func(old, pod *corev1.Pod) bool {
			return !equality.Semantic.DeepEqual(old.Spec.TopologySpreadConstraints, pod.Spec.TopologySpreadConstraints) || !equality.Semantic.DeepEqual(old.Labels, pod.Labels)
		}},
		{evaluate: h.validateAffinity, changed: func(old, pod *corev1.Pod) bool {
			return !equality.Semantic.DeepEqual(old.Spec.Affinity, pod.Spec.Affinity) || !equality.Semantic.DeepEqual(old.Labels, pod.Labels)
		}},
		{read: h.validatePriorityClasses, policy: appliesToPriorityClasses, changed: func(old, pod *corev1.Pod) bool {
			return old.Spec.PriorityClassName != pod.Spec.PriorityClassName
		}},
		{evaluate: h.validateResources},
		{evaluate: h.validateSchedulers, includeSubresources: true},
		{evaluate: h.validateQoSClasses, includeSubresources: true},
		{evaluate: h.validateRegistries, includeSubresources: true},
	}

	return h
}

func (h *podRules) OnCreate(
	_ client.Client,
	reader client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, req, reader, pod, tnt, recorder, enforceBodies); err != nil {
			return rejection("", err)
		}

		return nil
	}
}

func (h *podRules) OnUpdate(
	_ client.Client,
	reader client.Reader,
	old *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, req, reader, pod, tnt, recorder, enforceBodies, old); err != nil {
			return rejection("", err)
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

// rejection denies a rule decision and reports a failed class read as a server error.
func rejection(prefix string, err error) *admission.Response {
	if _, ok := errors.AsType[*classLookupError](err); ok {
		return handlers.ErroredResponse(err)
	}

	return ad.Deny(prefix + err.Error())
}

func (h *podRules) validatePodRules(
	ctx context.Context,
	req admission.Request,
	reader client.Reader,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
	old ...*corev1.Pod,
) error {
	enforceBodies = ruleengine.WorkloadEnforcement(enforceBodies, corev1.SchemeGroupVersion.WithKind("Pod"))

	return h.validateWorkloadRules(ctx, req, reader, pod, pod, pod, tnt, recorder, enforceBodies, old...)
}

func (h *podRules) validateWorkloadRules(
	ctx context.Context, req admission.Request, reader client.Reader, pod *corev1.Pod,
	object client.Object, conditionObject any, tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder, enforceBodies []*apirules.NamespaceRuleEnforceBody,
	old ...*corev1.Pod,
) error {
	conditional := false

	for _, body := range enforceBodies {
		if body != nil && len(body.Conditions) > 0 && hasWorkloadPolicy(body.Workloads, req.SubResource, pod == nil || pod.Spec.OS == nil || pod.Spec.OS.Name != corev1.Windows) {
			conditional = true

			break
		}
	}

	evaluator := ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest)

	var err error

	enforceBodies, err = ruleengine.FilterEnforcementConditions(ctx, evaluator, conditionObject, enforceBodies,
		func(body *apirules.NamespaceRuleEnforceBody) bool {
			return hasWorkloadPolicy(body.Workloads, req.SubResource, pod == nil || pod.Spec.OS == nil || pod.Spec.OS.Name != corev1.Windows)
		})
	if err != nil {
		return fmt.Errorf("enforce: %w", err)
	}

	for i := range h.rules {
		rule := &h.rules[i]
		if len(old) > 0 && old[0] != nil && rule.changed != nil && !rule.changed(old[0], pod) && !rule.recheck(conditional, enforceBodies) {
			continue
		}

		if req.SubResource != "" && !rule.includeSubresources {
			continue
		}

		if req.SubResource != "" && rule.subresource != "" && req.SubResource != rule.subresource {
			continue
		}

		evaluation, err := rule.run(ctx, reader, pod, enforceBodies)
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
				object,
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
					object,
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

// Placement and resource policies do not run on subresources.
func hasWorkloadPolicy(body apirules.NamespaceRuleEnforceWorkloadsBody, subresource string, linux bool) bool {
	if linux && (subresource == "" || subresource == "ephemeralcontainers") && (len(body.Security.SeccompProfiles) > 0 || len(body.Security.AppArmorProfiles) > 0) {
		return true
	}

	//nolint:staticcheck // Deprecated scheduler rules still activate workload enforcement.
	if len(body.Placement.Schedulers) > 0 || len(body.Schedulers) > 0 || len(body.QoSClasses) > 0 || len(body.Registries) > 0 {
		return true
	}

	return subresource == "" && (len(body.Placement.NodeSelector) > 0 || len(body.Placement.Tolerations) > 0 ||
		len(body.Placement.TopologySpreadConstraints) > 0 || len(body.Placement.Affinity) > 0 || body.Resources != nil || body.Classes.HasPolicies())
}
