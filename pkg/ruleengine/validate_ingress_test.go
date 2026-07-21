// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestValidateIngressRules(t *testing.T) {
	t.Parallel()

	valid := []*rules.NamespaceRuleBodyNamespace{{
		Enforce: &rules.NamespaceRuleEnforceBody{
			Ingress: rules.NamespaceRuleEnforceIngressBody{
				Types: []rules.IngressType{
					rules.IngressTypeIngress,
					rules.IngressTypeListenerSet,
					rules.IngressTypeHTTPRoute,
					rules.IngressTypeGateway,
					rules.IngressTypeTLSRoute,
					rules.IngressTypeGRPCRoute,
				},
				Hostnames: []runtime.ExpressionMatch{{
					Exact: []string{"prod", "test"},
					ExpressionRegex: runtime.ExpressionRegex{
						Expression: ".*\\.example\\.com",
					},
				}},
			},
		},
	}}
	if err := ValidateRuleStatusBody(nil, valid); err != nil {
		t.Fatalf("ValidateRuleStatusBody(valid) error = %v", err)
	}

	invalidType := valid[0].DeepCopy()
	invalidType.Enforce.Ingress.Types = []rules.IngressType{"TCPRoute"}
	if err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{invalidType}); err == nil || !strings.Contains(err.Error(), "unsupported ingress resource type") {
		t.Fatalf("ValidateRuleStatusBody(invalid type) error = %v", err)
	}

	invalidRegex := valid[0].DeepCopy()
	invalidRegex.Enforce.Ingress.Hostnames = []runtime.ExpressionMatch{{
		ExpressionRegex: runtime.ExpressionRegex{Expression: "("},
	}}
	if err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{invalidRegex}); err == nil || !strings.Contains(err.Error(), "ingress.hostnames[0].exp") {
		t.Fatalf("ValidateRuleStatusBody(invalid regex) error = %v", err)
	}
}
