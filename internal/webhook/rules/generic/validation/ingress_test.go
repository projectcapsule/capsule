// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestIngressTypeForGVK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		gvk  schema.GroupVersionKind
		want rules.IngressType
		ok   bool
	}{
		{schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}, rules.IngressTypeIngress, true},
		{schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}, rules.IngressTypeRoute, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway"}, rules.IngressTypeGateway, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "ListenerSet"}, rules.IngressTypeListenerSet, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}, rules.IngressTypeHTTPRoute, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "TLSRoute"}, rules.IngressTypeTLSRoute, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "GRPCRoute"}, rules.IngressTypeGRPCRoute, true},
		{schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "Gateway"}, "", false},
		{schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Ingress"}, "", false},
	}

	for _, tt := range tests {
		got, ok := ingressTypeForGVK(tt.gvk)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("ingressTypeForGVK(%s) = (%q, %v), want (%q, %v)", tt.gvk, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIngressHostnameEvaluationSupportsAllResourceShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType rules.IngressType
		spec         map[string]any
	}{
		{"Ingress", rules.IngressTypeIngress, map[string]any{
			"rules": []any{map[string]any{"host": "prod.example.com"}},
			"tls":   []any{map[string]any{"hosts": []any{"test.example.com"}}},
		}},
		{"OpenShift Route", rules.IngressTypeRoute, map[string]any{"host": "prod.example.com"}},
		{"Gateway", rules.IngressTypeGateway, listenerSpec("prod.example.com")},
		{"ListenerSet", rules.IngressTypeListenerSet, listenerSpec("test.example.com")},
		{"HTTPRoute", rules.IngressTypeHTTPRoute, routeSpec("prod.example.com")},
		{"TLSRoute", rules.IngressTypeTLSRoute, routeSpec("test.example.com")},
		{"GRPCRoute", rules.IngressTypeGRPCRoute, routeSpec("prod.example.com")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			evaluation, err := testIngressRules().evaluate(
				objectWithSpec(tt.spec),
				tt.resourceType,
				ingressRuleBodies(rules.ActionTypeAllow, tt.resourceType, runtime.ExpressionMatch{Exact: []string{"prod.example.com", "test.example.com"}}),
			)
			if err != nil {
				t.Fatalf("evaluate() error = %v", err)
			}
			if evaluation == nil || evaluation.Blocking != nil {
				t.Fatalf("evaluate() = %#v, want allowed", evaluation)
			}
		})
	}
}

func TestIngressHostnameEvaluationRegexAndAllowMiss(t *testing.T) {
	t.Parallel()

	body := ingressRuleBodies(
		rules.ActionTypeAllow,
		rules.IngressTypeHTTPRoute,
		runtime.ExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: ".*\\.example\\.com"}},
	)

	allowed, err := testIngressRules().evaluate(objectWithSpec(routeSpec("api.example.com")), rules.IngressTypeHTTPRoute, body)
	if err != nil || allowed == nil || allowed.Blocking != nil {
		t.Fatalf("allowed evaluation = %#v, err = %v", allowed, err)
	}

	denied, err := testIngressRules().evaluate(objectWithSpec(routeSpec("api.example.net")), rules.IngressTypeHTTPRoute, body)
	if err != nil {
		t.Fatalf("denied evaluation error = %v", err)
	}
	if denied == nil || denied.Blocking == nil || !strings.Contains(denied.Blocking.Message, "not allowed") {
		t.Fatalf("denied evaluation = %#v, want allow-list denial", denied)
	}
}

func TestIngressHostnameEvaluationRejectsMissingValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType rules.IngressType
		spec         map[string]any
		wantPath     string
	}{
		{"Ingress without rules", rules.IngressTypeIngress, map[string]any{}, "spec.rules[].host"},
		{"Ingress rule without host", rules.IngressTypeIngress, map[string]any{"rules": []any{map[string]any{}}}, "spec.rules[0].host"},
		{"OpenShift Route without host", rules.IngressTypeRoute, map[string]any{}, "spec.host"},
		{"Gateway listener without hostname", rules.IngressTypeGateway, listenerSpec(""), "spec.listeners[0].hostname"},
		{"ListenerSet listener without hostname", rules.IngressTypeListenerSet, listenerSpec(""), "spec.listeners[0].hostname"},
		{"HTTPRoute without hostnames", rules.IngressTypeHTTPRoute, map[string]any{}, "spec.hostnames[]"},
		{"TLSRoute without hostnames", rules.IngressTypeTLSRoute, map[string]any{}, "spec.hostnames[]"},
		{"GRPCRoute without hostnames", rules.IngressTypeGRPCRoute, map[string]any{}, "spec.hostnames[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			evaluation, err := testIngressRules().evaluate(
				objectWithSpec(tt.spec),
				tt.resourceType,
				ingressRuleBodies(rules.ActionTypeDeny, tt.resourceType, runtime.ExpressionMatch{Exact: []string{"prod.example.com"}}),
			)
			if err != nil {
				t.Fatalf("evaluate() error = %v", err)
			}
			if evaluation == nil || evaluation.Blocking == nil || !strings.Contains(evaluation.Blocking.Message, tt.wantPath) {
				t.Fatalf("evaluation = %#v, want missing hostname at %q", evaluation, tt.wantPath)
			}
		})
	}
}

func TestIngressHostnameEvaluationAuditsMissingValuesWithoutBlocking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceType rules.IngressType
		spec         map[string]any
		wantPath     string
	}{
		{"Ingress without rules", rules.IngressTypeIngress, map[string]any{}, "spec.rules[].host"},
		{"Ingress rule without host", rules.IngressTypeIngress, map[string]any{"rules": []any{map[string]any{}}}, "spec.rules[0].host"},
		{"OpenShift Route without host", rules.IngressTypeRoute, map[string]any{}, "spec.host"},
		{"Gateway listener without hostname", rules.IngressTypeGateway, listenerSpec(""), "spec.listeners[0].hostname"},
		{"HTTPRoute without hostnames", rules.IngressTypeHTTPRoute, map[string]any{}, "spec.hostnames[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evaluation, err := testIngressRules().evaluate(
				objectWithSpec(tt.spec),
				tt.resourceType,
				ingressRuleBodies(rules.ActionTypeAudit, tt.resourceType, runtime.ExpressionMatch{Exact: []string{"prod.example.com"}}),
			)
			if err != nil {
				t.Fatalf("evaluate() error = %v", err)
			}
			if evaluation == nil || evaluation.Blocking != nil {
				t.Fatalf("evaluation = %#v, want non-blocking audit", evaluation)
			}
			if len(evaluation.Audits) != 1 || !strings.Contains(evaluation.Audits[0].Message, tt.wantPath) {
				t.Fatalf("audits = %#v, want missing hostname audit at %q", evaluation.Audits, tt.wantPath)
			}
		})
	}
}

func TestIngressHostnameEvaluationAuditsAndRejectsMissingValueWithEnforcingRule(t *testing.T) {
	t.Parallel()

	bodies := append(
		ingressRuleBodies(rules.ActionTypeAudit, rules.IngressTypeGateway, runtime.ExpressionMatch{Exact: []string{"prod.example.com"}}),
		ingressRuleBodies(rules.ActionTypeAllow, rules.IngressTypeGateway, runtime.ExpressionMatch{Exact: []string{"prod.example.com"}})...,
	)

	evaluation, err := testIngressRules().evaluate(
		objectWithSpec(listenerSpec("")),
		rules.IngressTypeGateway,
		bodies,
	)
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if evaluation == nil || evaluation.Blocking == nil {
		t.Fatalf("evaluation = %#v, want blocking decision", evaluation)
	}
	if len(evaluation.Audits) != 1 || !strings.Contains(evaluation.Audits[0].Message, "empty hostname detected") {
		t.Fatalf("audits = %#v, want missing hostname audit", evaluation.Audits)
	}
}

func TestIngressHostnameEvaluationIgnoresUntargetedTypes(t *testing.T) {
	t.Parallel()

	evaluation, err := testIngressRules().evaluate(
		objectWithSpec(map[string]any{}),
		rules.IngressTypeGateway,
		ingressRuleBodies(rules.ActionTypeAllow, rules.IngressTypeIngress, runtime.ExpressionMatch{Exact: []string{"prod.example.com"}}),
	)
	if err != nil || evaluation != nil {
		t.Fatalf("evaluate() = %#v, err = %v, want nil", evaluation, err)
	}
}

func testIngressRules() *ingressRules {
	return &ingressRules{regexCache: cache.NewRegexCache()}
}

func objectWithSpec(spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
}

func listenerSpec(hostname string) map[string]any {
	listener := map[string]any{"name": "https"}
	if hostname != "" {
		listener["hostname"] = hostname
	}
	return map[string]any{"listeners": []any{listener}}
}

func routeSpec(hostname string) map[string]any {
	return map[string]any{"hostnames": []any{hostname}}
}

func ingressRuleBodies(action rules.ActionType, resourceType rules.IngressType, hostnames ...runtime.ExpressionMatch) []*rules.NamespaceRuleEnforceBody {
	return []*rules.NamespaceRuleEnforceBody{{
		Action: action,
		Ingress: rules.NamespaceRuleEnforceIngressBody{
			Types:     []rules.IngressType{resourceType},
			Hostnames: hostnames,
		},
	}}
}
