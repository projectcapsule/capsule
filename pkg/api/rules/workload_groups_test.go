// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestNestedWorkloadEnforcementSerialization(t *testing.T) {
	const source = `{"targets":["pod"],"disruptionBudgets":{"allowOverlap":false},"placement":{"schedulers":[{"exact":["team"]}],"nodeSelector":[{}]},"security":{"seccompProfiles":[{"types":["RuntimeDefault"]}],"appArmorProfiles":[{"types":["Localhost"],"localhostProfiles":[{"exact":["team-profile"]}]}]}}`
	var workload NamespaceRuleEnforceWorkloadsBody
	require.NoError(t, yaml.UnmarshalStrict([]byte(source), &workload))
	require.True(t, workload.HasPolicies())
	require.True(t, workload.HasPodSpecPolicies())
	require.False(t, workload.TargetsOnly())
	encoded, err := json.Marshal(workload.DeepCopy())
	require.NoError(t, err)
	require.JSONEq(t, source, string(encoded))
	copied := workload.DeepCopy()
	*copied.DisruptionBudgets.AllowOverlap = true
	copied.Placement.Schedulers[0].Exact[0] = "other"
	copied.Security.SeccompProfiles[0].Types[0] = SecurityProfileUnconfined
	copied.Security.AppArmorProfiles[0].LocalhostProfiles[0].Exact[0] = "other-profile"
	require.Equal(t, "team", workload.Placement.Schedulers[0].Exact[0])
	require.False(t, *workload.DisruptionBudgets.AllowOverlap)
	require.Equal(t, SecurityProfileRuntimeDefault, workload.Security.SeccompProfiles[0].Types[0])
	require.Equal(t, "team-profile", workload.Security.AppArmorProfiles[0].LocalhostProfiles[0].Exact[0])
	for _, source := range []string{
		`{"targets":["daemonset"]}`,
		`{"targets":["daemonset"],"placement":{},"security":{}}`,
		`{"targets":["daemonset"],"placement":null,"security":null}`,
		`{"targets":["daemonset"],"schedulers":[]}`,
		`{"targets":["daemonset"],"schedulers":null}`,
	} {
		var kindOnly NamespaceRuleEnforceWorkloadsBody
		require.NoError(t, yaml.UnmarshalStrict([]byte(source), &kindOnly))
		require.False(t, kindOnly.HasPolicies())
		require.True(t, kindOnly.TargetsOnly())
		encoded, err := json.Marshal(kindOnly)
		require.NoError(t, err)
		require.JSONEq(t, `{"targets":["daemonset"]}`, string(encoded))
	}
	for _, field := range []string{"seccompProfiles", "appArmorProfiles", "nodeSelector", "tolerations", "topologySpreadConstraints", "affinity"} {
		var workload NamespaceRuleEnforceWorkloadsBody
		require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"`+field+`":[]}`), &workload), "unknown field")
	}
}

func TestDeprecatedSchedulersSerialization(t *testing.T) {
	for _, source := range []string{
		`{"targets":["pod"],"schedulers":[{"exact":["legacy"]}]}`,
		`{"targets":["deployment"],"schedulers":[{"exact":["legacy"]}],"placement":{"schedulers":[{"exact":["preferred"]}]}}`,
	} {
		var workload NamespaceRuleEnforceWorkloadsBody
		require.NoError(t, yaml.UnmarshalStrict([]byte(source), &workload))
		require.True(t, workload.HasPodSpecPolicies())
		require.True(t, workload.HasPolicies())
		require.False(t, workload.TargetsOnly())
		copied := workload.DeepCopy()
		encoded, err := json.Marshal(copied)
		require.NoError(t, err)
		require.JSONEq(t, source, string(encoded))
		copied.Schedulers[0].Exact[0] = "changed"
		require.Equal(t, "legacy", workload.Schedulers[0].Exact[0])
	}
}
