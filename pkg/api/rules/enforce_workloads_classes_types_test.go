// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func TestWorkloadClassEnforcementRoundTripAndIsolation(t *testing.T) {
	source := `{"classes":{"priority":[{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"},"matchExpressions":[{"key":"tier","operator":"In","values":["gold"]}]}]}}`

	var original NamespaceRuleEnforceWorkloadsBody
	require.NoError(t, yaml.UnmarshalStrict([]byte(source), &original))

	match := original.Classes.Priority[0]
	require.Equal(t, []string{"internal.example.com"}, match.Exact)
	require.Equal(t, `^[a-z0-9-]+\.example\.com$`, match.Expression)
	require.Equal(t, map[string]string{"env": "customer"}, match.MatchLabels)
	require.True(t, match.HasNameMatch())
	require.True(t, match.HasSelector())

	encoded, err := json.Marshal(original.DeepCopy())
	require.NoError(t, err)
	require.JSONEq(t, source, string(encoded))

	copied := original.DeepCopy()
	copied.Classes.Priority[0].Exact[0] = "changed"
	copied.Classes.Priority[0].MatchLabels["env"] = "changed"
	copied.Classes.Priority[0].MatchExpressions[0].Values[0] = "changed"
	require.Equal(t, "internal.example.com", match.Exact[0])
	require.Equal(t, "customer", match.MatchLabels["env"])
	require.Equal(t, "gold", match.MatchExpressions[0].Values[0])
}

func TestWorkloadClassEnforcementOmission(t *testing.T) {
	for _, source := range []string{
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"]}}}`,
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":null}}}`,
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":{}}}}`,
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":{"priority":null}}}}`,
	} {
		var body NamespaceRuleBodyNamespace
		require.NoError(t, yaml.UnmarshalStrict([]byte(source), &body))
		require.False(t, body.Enforce.Workloads.Classes.HasPolicies(), source)

		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "classes", "old representations must not gain class rules")
	}

	var body NamespaceRuleBodyNamespace
	require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"enforce":{"workloads":{"priority":[]}}}`), &body), "unknown field")
}

func TestWorkloadClassEnforcementScopesTargets(t *testing.T) {
	classes := WorkloadClassEnforcement{Priority: []WorkloadClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}}
	deployment := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	pod := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}

	for _, tc := range []struct {
		name            string
		workloads       NamespaceRuleEnforceWorkloadsBody
		policies        bool
		targetsOnly     bool
		pod, deployment bool
	}{
		{name: "no classes with targets matches the kinds", workloads: NamespaceRuleEnforceWorkloadsBody{Targets: []WorkloadValidationTarget{ValidateDeployment}}, targetsOnly: true, deployment: true},
		{name: "classes without targets select pods", workloads: NamespaceRuleEnforceWorkloadsBody{Classes: classes}, policies: true, pod: true},
		{name: "classes scope controller targets", workloads: NamespaceRuleEnforceWorkloadsBody{Classes: classes, Targets: []WorkloadValidationTarget{ValidateDeployment}}, policies: true, deployment: true},
		{name: "classes scope both", workloads: NamespaceRuleEnforceWorkloadsBody{Classes: classes, Targets: []WorkloadValidationTarget{ValidatePod, ValidateDeployment}}, policies: true, pod: true, deployment: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.policies, tc.workloads.HasPodSpecPolicies())
			require.Equal(t, tc.policies, tc.workloads.HasPolicies())
			require.Equal(t, tc.targetsOnly, tc.workloads.TargetsOnly())

			_, matches := tc.workloads.PodTargets(pod)
			require.Equal(t, tc.pod, matches)

			_, matches = tc.workloads.PodTargets(deployment)
			require.Equal(t, tc.deployment, matches)
		})
	}
}

func TestWorkloadClassMutationRoundTripAndCopy(t *testing.T) {
	for _, tc := range []struct {
		source   string
		priority string
	}{
		{source: `{}`},
		{source: `{"classes":null}`},
		{source: `{"classes":{}}`},
		{source: `{"classes":{"priority":null}}`},
		{source: `{"classes":{"priority":"tenant-default"}}`, priority: "tenant-default"},
		{source: `{"classes":{"priority":"{{ .tenant.metadata.name }}-default"},"targets":["pod"]}`, priority: "{{ .tenant.metadata.name }}-default"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			var workload WorkloadMutation
			require.NoError(t, yaml.UnmarshalStrict([]byte(tc.source), &workload))
			require.Equal(t, tc.priority, workload.Classes.Priority)
			require.Equal(t, tc.priority != "", workload.HasPodProperties())
			require.Equal(t, tc.priority != "", workload.HasProperties())

			copied := workload.DeepCopy()
			require.Equal(t, workload, *copied)

			encoded, err := json.Marshal(copied)
			require.NoError(t, err)

			var decoded WorkloadMutation
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			require.Equal(t, workload, decoded)

			if tc.priority == "" {
				require.NotContains(t, string(encoded), "classes")
			}
		})
	}

	var workload WorkloadMutation
	require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"priority":"tenant-default"}`), &workload), "unknown field")
	require.Error(t, json.Unmarshal([]byte(`{"classes":{"priority":["tenant-default"]}}`), &workload))
}
