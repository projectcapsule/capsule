// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestListenerSetMutationValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  rules.GatewayParentReference
		want string
	}{
		{"defaults", rules.GatewayParentReference{Name: "edge"}, ""},
		{"explicit defaults", rules.GatewayParentReference{Name: "edge", Group: new("gateway.networking.k8s.io"), Kind: new("Gateway"), Namespace: new("platform")}, ""},
		{"extension parent", rules.GatewayParentReference{Name: "edge", Group: new("example.com"), Kind: new("CustomGateway")}, ""},
		{"explicit core group", rules.GatewayParentReference{Name: "edge", Group: new(""), Kind: new("Service")}, ""},
		{"templates", rules.GatewayParentReference{Name: "{{ .tenant.metadata.name }}", Namespace: new("{{ .namespace.metadata.name }}")}, ""},
		{"missing name", rules.GatewayParentReference{}, ".name"},
		{"invalid name", rules.GatewayParentReference{Name: "bad/name"}, ".name"},
		{"oversized name", rules.GatewayParentReference{Name: strings.Repeat("a", 254)}, ".name"},
		{"empty namespace", rules.GatewayParentReference{Name: "edge", Namespace: new("")}, ".namespace"},
		{"invalid namespace", rules.GatewayParentReference{Name: "edge", Namespace: new("bad.namespace")}, ".namespace"},
		{"invalid group", rules.GatewayParentReference{Name: "edge", Group: new("bad/group")}, ".group"},
		{"empty kind", rules.GatewayParentReference{Name: "edge", Kind: new("")}, ".kind"},
		{"invalid kind", rules.GatewayParentReference{Name: "edge", Kind: new("bad.kind")}, ".kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &tc.ref}}}}}
			before := body.DeepCopy()
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "rules[0].mutate[0].ingress.listenerSets.parentRef"+tc.want)
			}
			require.Equal(t, before, body)
		})
	}
	for _, mutation := range []rules.NamespaceRuleMutation{
		{},
		{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: "edge"}}}, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod}}},
	} {
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}), "at least one workload mutation property")
	}
}

func BenchmarkValidateListenerSetMutations(b *testing.B) {
	for _, count := range []int{1, 32} {
		for _, invalid := range []bool{false, true} {
			b.Run(fmt.Sprintf("rules=%d/invalid=%t", count, invalid), func(b *testing.B) {
				bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
				for i := range bodies {
					ref := &rules.GatewayParentReference{Name: "edge", Group: new("gateway.networking.k8s.io"), Kind: new("Gateway"), Namespace: new("platform")}
					if invalid && i == count-1 {
						ref.Name = "bad/name"
					}
					bodies[i] = &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: ref}}}}}
				}
				b.ReportAllocs()
				for b.Loop() {
					err := ValidateRuleStatusBody(nil, bodies)
					if (err != nil) != invalid {
						b.Fatalf("unexpected validation error: %v", err)
					}
				}
			})
		}
	}
}
