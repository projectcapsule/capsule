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
			"network": {"policies": {"egress": {"cidrs": ["0.0.0.0/0", "::/0"]}}}
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
			"egress": map[string]any{"cidrs": []any{"0.0.0.0/0", "::/0"}},
		},
	}, enforce["network"])

	copy := body.DeepCopy()
	copy.Enforce.Network.Policies.Egress.CIDRs[0] = "192.0.2.0/24"
	require.Equal(t, "0.0.0.0/0", body.Enforce.Network.Policies.Egress.CIDRs[0])

	for _, empty := range []string{`{}`, `{"enforce":{}}`, `{"enforce":{"network":{}}}`, `{"enforce":{"network":{"policies":{}}}}`} {
		var body NamespaceRuleBodyNamespace
		require.NoError(t, json.Unmarshal([]byte(empty), &body))
		if body.Enforce != nil {
			require.Nil(t, body.Enforce.Network.Policies.Egress)
		}
	}
}
