// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateIngressClassRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		body rules.NamespaceRuleEnforceIngressBody
		want string
	}{
		{"names", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{"edge"}}, {Expression: "^public-"}}}, ""},
		{"labels", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Classes: []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}}, ""},
		{"missing types", rules.NamespaceRuleEnforceIngressBody{Classes: []rules.IngressClassMatch{{Exact: []string{"edge"}}}}, "ingress.types"},
		{"no class reference", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeHTTPRoute}, Classes: []rules.IngressClassMatch{{Exact: []string{"edge"}}}}, "types must include Ingress or Gateway"},
		{"empty match", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Classes: []rules.IngressClassMatch{{}}}, "at least one of"},
		{"regex", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Classes: []rules.IngressClassMatch{{Expression: "("}}}, "classes[0].exp"},
		{"bad selector", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Invalid"}}}}}, "matchExpressions"},
		{"too many", rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeGateway}, Classes: make([]rules.IngressClassMatch, 65)}, "at most 64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Ingress: tc.body}}
			before := body.DeepCopy()
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			require.Equal(t, before, body)
		})
	}
}

func TestValidateIngressClassMutations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []rules.IngressType
		class string
		want  string
	}{
		{"both", []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, "edge", ""},
		{"template", []rules.IngressType{rules.IngressTypeGateway}, "{{ .tenant.metadata.name }}-edge", ""},
		{"no type", nil, "edge", "types and class"},
		{"no class", []rules.IngressType{rules.IngressTypeIngress}, "", "types and class"},
		{"other type", []rules.IngressType{rules.IngressTypeListenerSet}, "edge", "unsupported class mutation type"},
		{"duplicate", []rules.IngressType{rules.IngressTypeGateway, rules.IngressTypeGateway}, "edge", "duplicate type"},
		{"invalid name", []rules.IngressType{rules.IngressTypeIngress}, "bad/name", "invalid class name"},
		{"oversized", []rules.IngressType{rules.IngressTypeIngress}, strings.Repeat("a", 4097), "at most 4096"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{Types: tc.types, Class: tc.class}}}}
			before := body.DeepCopy()
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			require.Equal(t, before, body)
		})
	}
}
