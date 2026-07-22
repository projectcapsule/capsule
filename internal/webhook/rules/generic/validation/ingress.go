// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type ingressRules struct {
	regexCache *cache.RegexCache
}

func IngressRules(regexCache *cache.RegexCache) handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured] {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	return &ingressRules{regexCache: regexCache}
}

func (h *ingressRules) OnCreate(
	_ client.Client,
	_ client.Reader,
	obj *unstructured.Unstructured,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return h.validate(obj, recorder, tnt, bodies)
}

func (h *ingressRules) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *unstructured.Unstructured,
	obj *unstructured.Unstructured,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return h.validate(obj, recorder, tnt, bodies)
}

func (*ingressRules) OnDelete(
	client.Client,
	client.Reader,
	*unstructured.Unstructured,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	[]*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *ingressRules) validate(
	obj *unstructured.Unstructured,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}

		resourceType, supported := ingressTypeForGVK(gvk)
		if !supported {
			return nil
		}

		enforceBodies := ruleengine.EnforceBodiesFromNamespaceRules(bodies)

		evaluation, err := h.evaluate(obj, resourceType, enforceBodies)
		if err != nil {
			return ad.Deny(err.Error())
		}

		if evaluation == nil {
			return nil
		}

		for _, audit := range evaluation.Audits {
			recorder.LabeledEvent(
				obj,
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
			recorder.LabeledEvent(
				obj,
				corev1.EventTypeWarning,
				events.ReasonForbiddenIngressHostname,
				events.ActionValidationDenied,
				err.Error(),
			).
				WithRelated(tnt).
				WithTenantLabel(tnt).
				WithRequestAnnotations(req).
				Emit(ctx)

			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *ingressRules) evaluate(
	obj *unstructured.Unstructured,
	resourceType apirules.IngressType,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if obj == nil || !hasIngressHostnameRules(resourceType, enforceBodies) {
		return nil, nil
	}

	values, err := ingressHostnameValues(obj, resourceType)
	if err != nil {
		return nil, err
	}

	if len(values) == 0 {
		values = []ruleengine.Value{{Path: hostnameRootPath(resourceType)}}
	}

	evaluation := &ruleengine.Evaluation{}
	hasAuditRules := hasIngressHostnameRulesForAction(resourceType, enforceBodies, apirules.ActionTypeAudit)
	hasEnforcingRules := hasEnforcingIngressHostnameRules(resourceType, enforceBodies)

	for _, value := range values {
		if strings.TrimSpace(value.Value) != "" {
			continue
		}

		if hasAuditRules {
			evaluation.Audits = append(evaluation.Audits, missingHostnameAuditDecision(value, resourceType))
		}

		if hasEnforcingRules {
			evaluation.Blocking = missingHostnameDecision(value, resourceType)

			return evaluation, nil
		}
	}

	hostnameEvaluation, err := ruleengine.EvaluateEnforce(
		obj,
		enforceBodies,
		ruleengine.Set[runtime.ExpressionMatch, *unstructured.Unstructured]{
			Name:        "ingress hostname",
			EventReason: events.ReasonForbiddenIngressHostname,
			Values: func(*unstructured.Unstructured) []ruleengine.Value {
				return values
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []runtime.ExpressionMatch {
				if enforce == nil || !containsIngressType(enforce.Ingress.Types, resourceType) {
					return nil
				}

				return enforce.Ingress.Hostnames
			},
			Matches: func(match runtime.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
				matched, err := match.MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				return ruleengine.Match{Matched: matched, MatchedValue: runtime.DescribeExpressionMatch(match)}, nil
			},
			RuleDescription:    runtime.DescribeExpressionMatch,
			AllowedDescription: "Allowed hostnames",
		},
	)
	if err != nil {
		return nil, err
	}

	evaluation.Append(hostnameEvaluation)

	return evaluation, nil
}

func ingressTypeForGVK(gvk schema.GroupVersionKind) (apirules.IngressType, bool) {
	switch {
	case gvk.Group == "networking.k8s.io" && gvk.Version == "v1" && gvk.Kind == string(apirules.IngressTypeIngress):
		return apirules.IngressTypeIngress, true
	case gvk.Group == "route.openshift.io" && gvk.Version == "v1" && gvk.Kind == string(apirules.IngressTypeRoute):
		return apirules.IngressTypeRoute, true
	case gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1":
		resourceType := apirules.IngressType(gvk.Kind)
		switch resourceType {
		case apirules.IngressTypeListenerSet,
			apirules.IngressTypeHTTPRoute,
			apirules.IngressTypeGateway,
			apirules.IngressTypeTLSRoute,
			apirules.IngressTypeGRPCRoute:
			return resourceType, true
		}
	}

	return "", false
}

func hasIngressHostnameRules(resourceType apirules.IngressType, bodies []*apirules.NamespaceRuleEnforceBody) bool {
	for _, body := range bodies {
		if body != nil && containsIngressType(body.Ingress.Types, resourceType) && len(body.Ingress.Hostnames) > 0 {
			return true
		}
	}

	return false
}

func hasIngressHostnameRulesForAction(
	resourceType apirules.IngressType,
	bodies []*apirules.NamespaceRuleEnforceBody,
	action apirules.ActionType,
) bool {
	for _, body := range bodies {
		if body != nil &&
			body.Action.OrDefault() == action &&
			containsIngressType(body.Ingress.Types, resourceType) &&
			len(body.Ingress.Hostnames) > 0 {
			return true
		}
	}

	return false
}

func hasEnforcingIngressHostnameRules(
	resourceType apirules.IngressType,
	bodies []*apirules.NamespaceRuleEnforceBody,
) bool {
	for _, body := range bodies {
		if body != nil &&
			body.Action.OrDefault() != apirules.ActionTypeAudit &&
			containsIngressType(body.Ingress.Types, resourceType) &&
			len(body.Ingress.Hostnames) > 0 {
			return true
		}
	}

	return false
}

func containsIngressType(types []apirules.IngressType, expected apirules.IngressType) bool {
	return slices.Contains(types, expected)
}

func missingHostnameDecision(value ruleengine.Value, resourceType apirules.IngressType) *ruleengine.Decision {
	return &ruleengine.Decision{
		SetName:     "ingress hostname",
		EventReason: events.ReasonForbiddenIngressHostname,
		Action:      apirules.ActionTypeDeny,
		Value:       value,
		Message: fmt.Sprintf(
			"hostname is required at %s because hostname rules target %s",
			value.Path,
			resourceType,
		),
	}
}

func missingHostnameAuditDecision(value ruleengine.Value, resourceType apirules.IngressType) *ruleengine.Decision {
	return &ruleengine.Decision{
		SetName:     "ingress hostname",
		EventReason: events.ReasonNamespaceRuleAudit,
		Action:      apirules.ActionTypeAudit,
		Value:       value,
		Message: fmt.Sprintf(
			"empty hostname detected at %s for %s by audit namespace rule",
			value.Path,
			resourceType,
		),
	}
}

func hostnameRootPath(resourceType apirules.IngressType) string {
	switch resourceType {
	case apirules.IngressTypeIngress:
		return "spec.rules[].host"
	case apirules.IngressTypeRoute:
		return "spec.host"
	case apirules.IngressTypeGateway, apirules.IngressTypeListenerSet:
		return "spec.listeners[].hostname"
	default:
		return "spec.hostnames[]"
	}
}

func ingressHostnameValues(obj *unstructured.Unstructured, resourceType apirules.IngressType) ([]ruleengine.Value, error) {
	switch resourceType {
	case apirules.IngressTypeIngress:
		return ingressValues(obj)
	case apirules.IngressTypeRoute:
		return routeObjectValues(obj)
	case apirules.IngressTypeGateway, apirules.IngressTypeListenerSet:
		return listenerValues(obj)
	case apirules.IngressTypeHTTPRoute, apirules.IngressTypeTLSRoute, apirules.IngressTypeGRPCRoute:
		return routeValues(obj)
	default:
		return nil, nil
	}
}

func routeObjectValues(obj *unstructured.Unstructured) ([]ruleengine.Value, error) {
	host, _, err := unstructured.NestedString(obj.Object, "spec", "host")
	if err != nil {
		return nil, fmt.Errorf("read spec.host: %w", err)
	}

	return []ruleengine.Value{{Value: strings.TrimSpace(host), Path: "spec.host"}}, nil
}

func ingressValues(obj *unstructured.Unstructured) ([]ruleengine.Value, error) {
	values := make([]ruleengine.Value, 0)

	rules, found, err := unstructured.NestedSlice(obj.Object, "spec", "rules")
	if err != nil {
		return nil, fmt.Errorf("read spec.rules: %w", err)
	}

	if found {
		for i, item := range rules {
			rule, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("spec.rules[%d] is not an object", i)
			}

			host, _, err := unstructured.NestedString(rule, "host")
			if err != nil {
				return nil, fmt.Errorf("read spec.rules[%d].host: %w", i, err)
			}

			values = append(values, ruleengine.Value{Value: strings.TrimSpace(host), Path: fmt.Sprintf("spec.rules[%d].host", i)})
		}
	}

	tls, found, err := unstructured.NestedSlice(obj.Object, "spec", "tls")
	if err != nil {
		return nil, fmt.Errorf("read spec.tls: %w", err)
	}

	if found {
		for i, item := range tls {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("spec.tls[%d] is not an object", i)
			}

			hosts, hostsFound, err := unstructured.NestedStringSlice(entry, "hosts")
			if err != nil {
				return nil, fmt.Errorf("read spec.tls[%d].hosts: %w", i, err)
			}

			if !hostsFound || len(hosts) == 0 {
				values = append(values, ruleengine.Value{Path: fmt.Sprintf("spec.tls[%d].hosts", i)})

				continue
			}

			for j, host := range hosts {
				values = append(values, ruleengine.Value{Value: strings.TrimSpace(host), Path: fmt.Sprintf("spec.tls[%d].hosts[%d]", i, j)})
			}
		}
	}

	return values, nil
}

func listenerValues(obj *unstructured.Unstructured) ([]ruleengine.Value, error) {
	listeners, found, err := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	if err != nil {
		return nil, fmt.Errorf("read spec.listeners: %w", err)
	}

	if !found {
		return nil, nil
	}

	values := make([]ruleengine.Value, 0, len(listeners))

	for i, item := range listeners {
		listener, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("spec.listeners[%d] is not an object", i)
		}

		hostname, _, err := unstructured.NestedString(listener, "hostname")
		if err != nil {
			return nil, fmt.Errorf("read spec.listeners[%d].hostname: %w", i, err)
		}

		values = append(values, ruleengine.Value{Value: strings.TrimSpace(hostname), Path: fmt.Sprintf("spec.listeners[%d].hostname", i)})
	}

	return values, nil
}

func routeValues(obj *unstructured.Unstructured) ([]ruleengine.Value, error) {
	hostnames, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "hostnames")
	if err != nil {
		return nil, fmt.Errorf("read spec.hostnames: %w", err)
	}

	if !found {
		return nil, nil
	}

	values := make([]ruleengine.Value, 0, len(hostnames))
	for i, hostname := range hostnames {
		values = append(values, ruleengine.Value{Value: strings.TrimSpace(hostname), Path: fmt.Sprintf("spec.hostnames[%d]", i)})
	}

	return values, nil
}
