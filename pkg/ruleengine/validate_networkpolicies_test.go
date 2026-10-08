// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateNetworkPolicyCIDRs(t *testing.T) {
	for _, tc := range []struct {
		cidr  string
		valid bool
	}{
		{"0.0.0.0/0", true}, {"::/0", true}, {"10.20.3.7/16", true}, {"fd00:1234::/48", true},
		{"", false}, {"10.0.0.1", false}, {"10.0.0.0/33", false}, {"::/129", false}, {"::ffff:192.0.2.1/128", false},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: []string{tc.cidr}}}}}}
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "rules[0].enforce.network.policies.egress.cidrs[0]")
			}
		})
	}
	require.NoError(t, validateNetworkPolicyRules(0, rules.NamespaceRuleEnforceNetworkPoliciesBody{}))
	require.NoError(t, validateNetworkPolicyRules(0, rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{}}))
}
