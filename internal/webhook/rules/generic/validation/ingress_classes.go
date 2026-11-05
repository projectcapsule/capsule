// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/projectcapsule/capsule/internal/webhook/ingress"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func appliesIngressClasses(body *rules.NamespaceRuleEnforceBody, kind rules.IngressType) bool {
	return (kind == rules.IngressTypeIngress || kind == rules.IngressTypeGateway) &&
		body != nil && len(body.Ingress.Classes) > 0 && containsIngressType(body.Ingress.Types, kind)
}

func (h *ingressRules) evaluateClasses(ctx context.Context, reader client.Reader, obj *unstructured.Unstructured, kind rules.IngressType, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	if obj == nil || !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool { return appliesIngressClasses(body, kind) }) {
		return nil, nil
	}

	refs, err := ingress.ReadClassReferences(obj, kind)
	if err != nil {
		return nil, err
	}

	values := make([]ruleengine.Value, 0, 2)
	if refs.Name != "" {
		values = append(values, ruleengine.Value{Value: refs.Name, Path: "spec." + refs.Field})
	}

	if refs.Annotation != "" && refs.Annotation != refs.Name {
		values = append(values, ruleengine.Value{Value: refs.Annotation, Path: "metadata.annotations[" + meta.IngressClassAnnotation + "]"})
	}

	if len(values) == 0 {
		return missingIngressClass(kind, refs.Field, bodies), nil
	}

	evaluation := &ruleengine.Evaluation{}

	for _, value := range values {
		result, err := h.evaluateClassReference(ctx, reader, obj, kind, value, bodies)
		if err != nil {
			return nil, err
		}

		evaluation.Append(result)
	}

	return evaluation, nil
}

func missingIngressClass(kind rules.IngressType, field string, bodies []*rules.NamespaceRuleEnforceBody) *ruleengine.Evaluation {
	evaluation := &ruleengine.Evaluation{}
	name := strings.ToLower(string(kind)) + " class"

	reason := events.ReasonMissingIngressClass
	if kind == rules.IngressTypeGateway {
		reason = events.ReasonMissingGatewayClass
	}

	for _, body := range bodies {
		if !appliesIngressClasses(body, kind) {
			continue
		}

		decision := &ruleengine.Decision{
			SetName: name, EventReason: reason, Value: ruleengine.Value{Path: "spec." + field},
			Action: body.Action.OrDefault(), Message: fmt.Sprintf("%s is required at spec.%s by namespace class rules", name, field),
		}
		if decision.Action == rules.ActionTypeAudit {
			evaluation.Audits = append(evaluation.Audits, decision)
		} else {
			evaluation.Blocking = decision
		}
	}

	return evaluation
}

func (h *ingressRules) evaluateClassReference(ctx context.Context, reader client.Reader, obj *unstructured.Unstructured, kind rules.IngressType, value ruleengine.Value, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	matcher := ingressClassMatcher{handler: h, reader: reader, kind: kind}

	reason := events.ReasonForbiddenIngressClass
	if kind == rules.IngressTypeGateway {
		reason = events.ReasonForbiddenGatewayClass
	}

	return ruleengine.EvaluateEnforce(obj, bodies, ruleengine.Set[rules.IngressClassMatch, *unstructured.Unstructured]{
		Name: strings.ToLower(string(kind)) + " class", EventReason: reason,
		Values: func(*unstructured.Unstructured) []ruleengine.Value { return []ruleengine.Value{value} },
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.IngressClassMatch {
			if !appliesIngressClasses(body, kind) {
				return nil
			}

			// EvaluateEnforce visits each body's entries before advancing. Audit
			// can observe an unresolved reference; enforcement needs known labels.
			matcher.enforcing = body.Action.OrDefault() != rules.ActionTypeAudit

			return h.classMatchesForName(body, value.Value)
		},
		Matches: func(match rules.IngressClassMatch, value ruleengine.Value) (ruleengine.Match, error) {
			return matcher.match(ctx, match, value.Value)
		},
		RuleDescription: rules.IngressClassMatch.Describe, AllowedDescription: "Allowed classes",
	})
}

// Keep one request-local read per class reference. Neither objects nor match
// results enter a process-wide cache; only immutable expressions are shared.
type ingressClassMatcher struct {
	handler   *ingressRules
	reader    client.Reader
	kind      rules.IngressType
	class     client.Object
	read      bool
	enforcing bool
}

func (m *ingressClassMatcher) match(ctx context.Context, match rules.IngressClassMatch, name string) (ruleengine.Match, error) {
	if match.HasNameMatch() {
		matched, err := match.NameMatch().MatchesWithExpressionMatcher(m.handler.regexCache, name)
		if err != nil || matched {
			return ruleengine.Match{Matched: matched, MatchedValue: match.Describe()}, err
		}
	}

	if !match.HasSelector() {
		return ruleengine.Match{}, nil
	}

	selector, err := m.handler.selectors.GetOrCompile(&match.LabelSelector)
	if err != nil {
		return ruleengine.Match{}, err
	}

	class, err := m.get(ctx, name)
	if err != nil {
		return ruleengine.Match{}, err
	}

	if class == nil && m.enforcing {
		return ruleengine.Match{}, fmt.Errorf("%sClass %q cannot be resolved for class selector enforcement", m.kind, name)
	}

	return ruleengine.Match{Matched: class != nil && selector.Matches(labels.Set(class.GetLabels())), MatchedValue: match.Describe()}, nil
}

func (m *ingressClassMatcher) get(ctx context.Context, name string) (client.Object, error) {
	if m.read {
		return m.class, nil
	}

	if m.reader == nil {
		return nil, fmt.Errorf("%sClass selector reader is not configured", m.kind)
	}

	m.read = true

	m.class = &networkingv1.IngressClass{}

	if m.kind == rules.IngressTypeGateway {
		m.class = &gatewayv1.GatewayClass{}
	}

	if err := m.reader.Get(ctx, types.NamespacedName{Name: name}, m.class); err != nil {
		m.class = nil
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("reading %sClass %q: %w", m.kind, name, err)
		}
	}

	return m.class, nil
}

// Resolve name matches before selectors for OR entries. A matching name must
// not require unrelated API reads, or fail because an unnecessary read failed.
func (h *ingressRules) classMatchesForName(body *rules.NamespaceRuleEnforceBody, name string) []rules.IngressClassMatch {
	if body.Action.OrDefault() != rules.ActionTypeAudit {
		for i, match := range body.Ingress.Classes {
			if !match.HasNameMatch() {
				continue
			}

			matched, err := match.NameMatch().MatchesWithExpressionMatcher(h.regexCache, name)
			if err != nil {
				return body.Ingress.Classes
			}

			if matched {
				return body.Ingress.Classes[i : i+1]
			}
		}
	}

	return body.Ingress.Classes
}
