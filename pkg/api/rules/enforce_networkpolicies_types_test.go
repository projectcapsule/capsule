// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetworkPolicyEnforcementJSONAndDeepCopy(t *testing.T) {
	var body NamespaceRuleBodyNamespace
	require.NoError(t, json.Unmarshal([]byte(`{
		"enforce": {
			"action": "deny",
			"network": {"policies": {"ingress": {"cidrs": ["192.0.2.0/24"]}, "egress": {"cidrs": ["0.0.0.0/0", "::/0"]}}}
		}
	}`), &body))
	require.NotNil(t, body.Enforce)
	require.NotNil(t, body.Enforce.Network.Policies.Egress)
	require.Equal(t, []string{"0.0.0.0/0", "::/0"}, body.Enforce.Network.Policies.Egress.CIDRs)

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var encoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &encoded))
	enforce := encoded["enforce"].(map[string]any)
	require.NotContains(t, enforce, "networkPolicies")
	require.Equal(t, map[string]any{
		"policies": map[string]any{
			"ingress": map[string]any{"cidrs": []any{"192.0.2.0/24"}},
			"egress":  map[string]any{"cidrs": []any{"0.0.0.0/0", "::/0"}},
		},
	}, enforce["network"])

	require.Equal(t, []string{"192.0.2.0/24"}, body.Enforce.Network.Policies.Ingress.CIDRs)
	copy := body.DeepCopy()
	copy.Enforce.Network.Policies.Ingress.CIDRs[0] = "::/0"
	require.Equal(t, "192.0.2.0/24", body.Enforce.Network.Policies.Ingress.CIDRs[0])
	copy.Enforce.Network.Policies.Egress.CIDRs[0] = "192.0.2.0/24"
	require.Equal(t, "0.0.0.0/0", body.Enforce.Network.Policies.Egress.CIDRs[0])

	for _, empty := range []string{`{}`, `{"enforce":{}}`, `{"enforce":{"network":{}}}`, `{"enforce":{"network":{"policies":{}}}}`} {
		var body NamespaceRuleBodyNamespace
		require.NoError(t, json.Unmarshal([]byte(empty), &body))
		if body.Enforce != nil {
			require.Nil(t, body.Enforce.Network.Policies.Egress)
			require.Nil(t, body.Enforce.Network.Policies.Ingress)
		}
	}
}

func TestNetworkPolicyLegacyEgress(t *testing.T) {
	var body NamespaceRuleBodyNamespace
	require.NoError(t, json.Unmarshal([]byte(`{"enforce":{"network":{"policies":{"egress":{"cidrs":["10.0.0.0/8"]}}}}}`), &body))
	require.Nil(t, body.Enforce.Network.Policies.Ingress)
	require.Equal(t, []string{"10.0.0.0/8"}, body.Enforce.Network.Policies.Egress.CIDRs)
	for _, raw := range []string{`{}`, `{"ingress":{}}`, `{"ingress":{"cidrs":[]}}`} {
		var policy NamespaceRuleEnforceNetworkPoliciesBody
		require.NoError(t, json.Unmarshal([]byte(raw), &policy))
		copy := policy.DeepCopy()
		require.Equal(t, policy, *copy)
		if policy.Ingress != nil {
			require.Empty(t, policy.Ingress.CIDRs)
		}
	}
}
