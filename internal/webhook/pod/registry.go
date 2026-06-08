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
	rule *rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder, rule)
	}
}

func (h *registryHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	old *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	rule *rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder, rule)
	}
}

func (h *registryHandler) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *registryHandler) validate(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	rule *rules.NamespaceRuleBodyNamespace,
) *admission.Response {
	if h.cache == nil {
		resp := admission.Errored(http.StatusInternalServerError, fmt.Errorf("registry rule set cache is nil"))

		return &resp
	}

	if rule == nil || len(rule.Enforce.Registries) == 0 {
		resp := admission.Allowed("no registry rules")

		return &resp
	}

	rs, _, err := h.cache.GetOrBuild(rule.Enforce.Registries)
	if err != nil {
		resp := admission.Errored(http.StatusInternalServerError, err)

		return &resp
	}

	if rs == nil {
		resp := admission.Allowed("no registry rules")

		return &resp
	}

	if rs.HasImages {
		if resp := h.validateContainers(req, pod, tnt, recorder, rule, rs); resp != nil {
			return resp
		}
	}

	if rs.HasVolumes {
		if resp := h.validateVolumes(req, pod, tnt, recorder, rule, rs); resp != nil {
			return resp
		}
	}

	return nil
}

func (h *registryHandler) validateContainers(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	rule *rules.NamespaceRuleBodyNamespace,
	rs *cache.RuleSet,
) *admission.Response {
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]

		if resp := h.verifyOCIReference(
			recorder,
			req,
			tnt,
			pod,
			rule,
			rs,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("initContainers[%d]", i),
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
			rule,
			rs,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("ephemeralContainers[%d]", i),
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
			rule,
			rs,
			rules.ValidateImages,
			c.Image,
			c.ImagePullPolicy,
			fmt.Sprintf("containers[%d]", i),
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
	rule *rules.NamespaceRuleBodyNamespace,
	rs *cache.RuleSet,
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
			rule,
			rs,
			rules.ValidateVolumes,
			ref,
			v.Image.PullPolicy,
			fmt.Sprintf("volumes[%d](%s)", i, v.Name),
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
	rule *rules.NamespaceRuleBodyNamespace,
	rs *cache.RuleSet,
	target rules.RegistryValidationTarget,
	reference string,
	pullPolicy corev1.PullPolicy,
	where string,
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

	matched, err := h.cache.MatchReference(rs, ref, target)
	if err != nil {
		resp := admission.Errored(http.StatusInternalServerError, err)

		return &resp
	}

	if matched == nil {
		return nil
	}

	action := rule.Enforce.Action
	if action == "" {
		action = rules.ActionTypeDeny
	}

	switch action {
	case rules.ActionTypeAllow:
		if resp := h.validateAllowedPullPolicy(recorder, tnt, pod, matched, ref, pullPolicy, where); resp != nil {
			return resp
		}

		return nil

	case rules.ActionTypeAudit:
		h.auditWithEvent(
			recorder,
			tnt,
			pod,
			fmt.Sprintf(
				"%s reference %q matched audit registry rule %q",
				where,
				ref,
				matched.Expression.Expression,
			),
		)

		return nil

	case rules.ActionTypeDeny:
		msg := fmt.Sprintf(
			"%s reference %q is denied by registry rule %q",
			where,
			ref,
			matched.Expression.Expression,
		)

		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenContainerRegistry,
			msg,
		)

	default:
		resp := admission.Errored(
			http.StatusInternalServerError,
			fmt.Errorf("unsupported namespace rule action %q", action),
		)

		return &resp
	}
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
