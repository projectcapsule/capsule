// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type registryHandler struct {
	configuration configuration.Configuration
	cache         *cache.RegistryRuleSetCache
}

func ContainerRegistry(configuration configuration.Configuration, cache *cache.RegistryRuleSetCache) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &registryHandler{
		configuration: configuration,
		cache:         cache,
	}
}

func (h *registryHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, pod, tnt, recorder, ruleBlocks)
	}
}

func (h *registryHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(ctx, req, pod, tnt, recorder, ruleBlocks)
	}
}

func (h *registryHandler) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	[]*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *registryHandler) validate(
	ctx context.Context,
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) *admission.Response {
	if h.cache == nil {
		resp := admission.Errored(http.StatusInternalServerError, fmt.Errorf("registry rule set cache is nil"))

		return &resp
	}

	log.FromContext(ctx).V(5).Info(
		"handling pod registry rules",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"rules", len(ruleBlocks),
	)

	if len(ruleBlocks) == 0 {
		return nil
	}

	warnings := make([]string, 0)

	if resp := h.validateContainers(req, pod, tnt, recorder, ruleBlocks, &warnings); resp != nil {
		return resp
	}

	if resp := h.validateVolumes(req, pod, tnt, recorder, ruleBlocks, &warnings); resp != nil {
		return resp
	}

	if len(warnings) > 0 {
		resp := admission.Allowed("registry rules audited")
		resp.Warnings = append(resp.Warnings, warnings...)

		return &resp
	}

	return nil
}

func (h *registryHandler) validateContainers(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	warnings *[]string,
) *admission.Response {
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]

		if resp := h.verifyOCIReference(
			recorder,
			req,
			tnt,
			pod,
			ruleBlocks,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("initContainers[%d]", i),
			warnings,
		); resp != nil {
			return resp
		}
	}

	for i := range pod.Spec.EphemeralContainers {
		c := pod.Spec.EphemeralContainers[i]

		if resp := h.verifyOCIReference(
			recorder,
			req,
			tnt,
			pod,
			ruleBlocks,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("ephemeralContainers[%d]", i),
			warnings,
		); resp != nil {
			return resp
		}
	}

	for i := range pod.Spec.Containers {
		c := pod.Spec.Containers[i]

		if resp := h.verifyOCIReference(
			recorder,
			req,
			tnt,
			pod,
			ruleBlocks,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("containers[%d]", i),
			warnings,
		); resp != nil {
			return resp
		}
	}

	return nil
}

func (h *registryHandler) validateVolumes(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	warnings *[]string,
) *admission.Response {
	for i := range pod.Spec.Volumes {
		v := pod.Spec.Volumes[i]
		if v.Image == nil {
			continue
		}

		ref := strings.TrimSpace(v.Image.Reference)
		if ref == "" {
			return h.denyWithEvent(
				recorder,
				tnt,
				pod,
				evt.ReasonForbiddenContainerRegistry,
				fmt.Sprintf("volume %q has empty image.reference", v.Name),
			)
		}

		if resp := h.verifyOCIReference(
			recorder,
			req,
			tnt,
			pod,
			ruleBlocks,
			rules.ValidateVolumes,
			ref,
			v.Image.PullPolicy,
			fmt.Sprintf("volumes[%d](%s)", i, v.Name),
			warnings,
		); resp != nil {
			return resp
		}
	}

	return nil
}

func (h *registryHandler) verifyOCIReference(
	recorder events.EventRecorder,
	req admission.Request,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	target rules.RegistryValidationTarget,
	reference string,
	pullPolicy corev1.PullPolicy,
	where string,
	warnings *[]string,
) *admission.Response {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenContainerRegistry,
			fmt.Sprintf("%s has empty reference", where),
		)
	}

	evaluation, err := h.evaluateOCIReference(ruleBlocks, target, ref)
	if err != nil {
		resp := admission.Errored(http.StatusInternalServerError, err)

		return &resp
	}

	if evaluation == nil {
		return nil
	}

	for _, audit := range evaluation.Audits {
		msg := fmt.Sprintf(
			"%s reference %q matched audit registry rule %q",
			where,
			ref,
			audit.Matched.Expression.Expression,
		)

		h.auditWithEvent(recorder, tnt, pod, msg)

		if warnings != nil {
			*warnings = append(*warnings, msg)
		}
	}

	if evaluation.Decision == nil {
		return nil
	}

	switch evaluation.Decision.Action {
	case rules.ActionTypeAllow:
		if resp := h.validateAllowedPullPolicy(
			recorder,
			tnt,
			pod,
			evaluation.Decision.Matched,
			ref,
			pullPolicy,
			where,
		); resp != nil {
			return resp
		}

		return nil

	case rules.ActionTypeDeny:
		msg := fmt.Sprintf(
			"%s reference %q is denied by registry rule %q",
			where,
			ref,
			evaluation.Decision.Matched.Expression.Expression,
		)

		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenContainerRegistry,
			msg,
		)

	case rules.ActionTypeAudit:
		msg := fmt.Sprintf(
			"%s reference %q matched audit registry rule %q",
			where,
			ref,
			evaluation.Decision.Matched.Expression.Expression,
		)

		h.auditWithEvent(recorder, tnt, pod, msg)

		if warnings != nil {
			*warnings = append(*warnings, msg)
		}

		return nil

	default:
		resp := admission.Errored(
			http.StatusInternalServerError,
			fmt.Errorf("unsupported namespace rule action %q", evaluation.Decision.Action),
		)

		return &resp
	}
}

type registryDecision struct {
	rules.RuleDecision

	Matched *cache.CompiledRule
}

type registryEvaluation struct {
	Decision *registryDecision
	Audits   []*registryDecision
}

func (h *registryHandler) evaluateOCIReference(
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	target rules.RegistryValidationTarget,
	ref string,
) (*registryEvaluation, error) {
	evaluation := &registryEvaluation{}

	for _, rule := range ruleBlocks {
		if rule == nil || len(rule.Enforce.Registries) == 0 {
			continue
		}

		rs, _, err := h.cache.GetOrBuild(rule.Enforce.Registries)
		if err != nil {
			return nil, err
		}

		if rs == nil {
			continue
		}

		matched, err := h.cache.MatchReference(rs, ref, target)
		if err != nil {
			return nil, err
		}

		if matched == nil {
			continue
		}

		action := rule.Enforce.Action
		if action == "" {
			action = rules.ActionTypeDeny
		}

		decision := &registryDecision{
			RuleDecision: rules.RuleDecision{
				Action: action,
				Rule:   rule,
			},
			Matched: matched,
		}

		switch action {
		case rules.ActionTypeAllow, rules.ActionTypeDeny:
			// Last matching allow/deny wins.
			evaluation.Decision = decision

		case rules.ActionTypeAudit:
			evaluation.Audits = append(evaluation.Audits, decision)

		default:
			return nil, fmt.Errorf("unsupported namespace rule action %q", action)
		}
	}

	return evaluation, nil
}

func (h *registryHandler) validateAllowedPullPolicy(
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	matched *cache.CompiledRule,
	ref string,
	pullPolicy corev1.PullPolicy,
	where string,
) *admission.Response {
	if matched == nil || len(matched.AllowedPolicy) == 0 {
		return nil
	}

	allowed := formatAllowedPullPolicies(matched.AllowedPolicy)

	if pullPolicy == "" {
		msg := fmt.Sprintf(
			"%s reference %q must explicitly set pullPolicy (allowed: %s)",
			where,
			ref,
			allowed,
		)

		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenPullPolicy,
			msg,
		)
	}

	if _, ok := matched.AllowedPolicy[pullPolicy]; !ok {
		msg := fmt.Sprintf(
			"%s reference %q uses pullPolicy=%s which is not allowed (allowed: %s)",
			where,
			ref,
			pullPolicy,
			allowed,
		)

		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenPullPolicy,
			msg,
		)
	}

	return nil
}

func (h *registryHandler) auditWithEvent(
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	msg string,
) {
	recorder.Eventf(
		pod,
		tnt,
		corev1.EventTypeWarning,
		evt.ReasonForbiddenContainerRegistry,
		evt.ActionValidationDenied,
		msg,
	)
}

func (h *registryHandler) denyWithEvent(
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	reason string,
	msg string,
) *admission.Response {
	recorder.Eventf(
		pod,
		tnt,
		corev1.EventTypeWarning,
		reason,
		evt.ActionValidationDenied,
		msg,
	)

	return ad.Deny(msg)
}

func formatAllowedPullPolicies(policies map[corev1.PullPolicy]struct{}) string {
	if len(policies) == 0 {
		return ""
	}

	out := make([]string, 0, len(policies))
	for p := range policies {
		out = append(out, string(p))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}
