// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateNetworkPolicyCIDRs(t *testing.T) {
	for _, direction := range []string{"egress", "ingress"} {
		for _, tc := range []struct {
			cidr  string
			valid bool
		}{
			{"0.0.0.0/0", true}, {"::/0", true}, {"10.20.3.7/16", true}, {"fd00:1234::/48", true},
			{"", false}, {"10.0.0.1", false}, {"10.0.0.0/33", false}, {"::/129", false}, {"::ffff:192.0.2.1/128", false},
		} {
			t.Run(direction+"/"+tc.cidr, func(t *testing.T) {
				body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: []string{tc.cidr}}}}}}
				if direction == "ingress" {
					body.Enforce.Network.Policies.Ingress, body.Enforce.Network.Policies.Egress = body.Enforce.Network.Policies.Egress, nil
				}
				err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "rules[0].enforce.network.policies."+direction+".cidrs[0]")
				}
			})
		}
	}
	require.NoError(t, validateNetworkPolicyRules(0, rules.NamespaceRuleEnforceNetworkPoliciesBody{}))
	require.NoError(t, validateNetworkPolicyRules(0, rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{}, Ingress: &rules.NetworkPolicyCIDRRule{}}))
}

func BenchmarkValidateNetworkPolicyCIDRs(b *testing.B) {
	for _, direction := range []string{"egress", "ingress", "both"} {
		for _, count := range []int{1, 16} {
			for _, outcome := range []string{"valid", "invalid", "skip"} {
				b.Run(fmt.Sprintf("%s/rules=%d/%s", direction, count, outcome), func(b *testing.B) {
					bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
					for i := range bodies {
						cidrs := make([]string, 64)
						for j := range cidrs {
							cidrs[j] = fmt.Sprintf("10.%d.%d.0/24", i, j)
						}
						if outcome == "invalid" && i == count-1 {
							cidrs[63] = "invalid"
						}
						configured := rules.NamespaceRuleEnforceNetworkPoliciesBody{}
						if outcome != "skip" {
							if direction != "ingress" {
								configured.Egress = &rules.NetworkPolicyCIDRRule{CIDRs: cidrs}
							}
							if direction != "egress" {
								configured.Ingress = &rules.NetworkPolicyCIDRRule{CIDRs: cidrs}
							}
						}
						bodies[i] = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Network: rules.NamespaceRuleEnforceNetworkBody{Policies: configured}}}
					}
					b.ReportAllocs()
					for b.Loop() {
						err := ValidateRuleStatusBody(nil, bodies)
						if (err != nil) != (outcome == "invalid") {
							b.Fatalf("unexpected validation: %v", err)
						}
					}
				})
			}
		}
	}
}
