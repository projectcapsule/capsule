// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// workloadClassFields lists the class kinds of a workload rule with accessors
// for their enforcement matchers and their mutation default.
var workloadClassFields = []struct {
	name     string
	matches  func(*WorkloadClassEnforcement) *[]WorkloadClassMatch
	assigned func(*WorkloadClassMutation) *string
}{
	{name: "priority", matches: func(c *WorkloadClassEnforcement) *[]WorkloadClassMatch { return &c.Priority }, assigned: func(c *WorkloadClassMutation) *string { return &c.Priority }},
	{name: "runtime", matches: func(c *WorkloadClassEnforcement) *[]WorkloadClassMatch { return &c.Runtime }, assigned: func(c *WorkloadClassMutation) *string { return &c.Runtime }},
}

func TestWorkloadClassEnforcementRoundTripAndIsolation(t *testing.T) {
	for _, field := range workloadClassFields {
		t.Run(field.name, func(t *testing.T) {
			source := `{"classes":{"` + field.name + `":[{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"},"matchExpressions":[{"key":"tier","operator":"In","values":["gold"]}]}]}}`

			var original NamespaceRuleEnforceWorkloadsBody
			require.NoError(t, yaml.UnmarshalStrict([]byte(source), &original))

			match := (*field.matches(&original.Classes))[0]
			require.Equal(t, []string{"internal.example.com"}, match.Exact)
			require.Equal(t, `^[a-z0-9-]+\.example\.com$`, match.Expression)
			require.Equal(t, map[string]string{"env": "customer"}, match.MatchLabels)
			require.True(t, match.HasNameMatch())
			require.True(t, match.HasSelector())

			encoded, err := json.Marshal(original.DeepCopy())
			require.NoError(t, err)
			require.JSONEq(t, source, string(encoded))

			copied := original.DeepCopy()
			copiedMatch := &(*field.matches(&copied.Classes))[0]
			copiedMatch.Exact[0] = "changed"
			copiedMatch.MatchLabels["env"] = "changed"
			copiedMatch.MatchExpressions[0].Values[0] = "changed"
			require.Equal(t, "internal.example.com", match.Exact[0])
			require.Equal(t, "customer", match.MatchLabels["env"])
			require.Equal(t, "gold", match.MatchExpressions[0].Values[0])
		})
	}

	// The class kinds are independent lists of one rule.
	var both NamespaceRuleEnforceWorkloadsBody
	require.NoError(t, yaml.UnmarshalStrict([]byte(`{"classes":{"priority":[{"exact":["gold"]}],"runtime":[{"exact":["kata"]}]}}`), &both))
	require.Equal(t, []WorkloadClassMatch{{Exact: []string{"gold"}}}, both.Classes.Priority)
	require.Equal(t, []WorkloadClassMatch{{Exact: []string{"kata"}}}, both.Classes.Runtime)
}

func TestWorkloadClassEnforcementOmission(t *testing.T) {
	sources := []string{
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"]}}}`,
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":null}}}`,
		`{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":{}}}}`,
	}
	for _, field := range workloadClassFields {
		sources = append(sources, `{"enforce":{"action":"deny","workloads":{"qosClasses":["BestEffort"],"classes":{"`+field.name+`":null}}}}`)
	}

	for _, source := range sources {
		var body NamespaceRuleBodyNamespace
		require.NoError(t, yaml.UnmarshalStrict([]byte(source), &body))
		require.False(t, body.Enforce.Workloads.Classes.HasPolicies(), source)

		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "classes", "old representations must not gain class rules")
	}

	for _, field := range workloadClassFields {
		var body NamespaceRuleBodyNamespace
		require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"enforce":{"workloads":{"`+field.name+`":[]}}}`), &body), "unknown field")
	}
}

func TestWorkloadClassEnforcementScopesTargets(t *testing.T) {
	deployment := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	pod := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}

	for _, field := range workloadClassFields {
		var classes WorkloadClassEnforcement
		*field.matches(&classes) = []WorkloadClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}
		require.True(t, classes.HasPolicies())

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
			t.Run(field.name+"/"+tc.name, func(t *testing.T) {
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
}

func TestWorkloadClassMutationRoundTripAndCopy(t *testing.T) {
	for _, field := range workloadClassFields {
		for _, tc := range []struct {
			source string
			class  string
		}{
			{source: `{}`},
			{source: `{"classes":null}`},
			{source: `{"classes":{}}`},
			{source: `{"classes":{"NAME":null}}`},
			{source: `{"classes":{"NAME":"tenant-default"}}`, class: "tenant-default"},
			{source: `{"classes":{"NAME":"{{ .tenant.metadata.name }}-default"},"targets":["pod"]}`, class: "{{ .tenant.metadata.name }}-default"},
		} {
			source := strings.ReplaceAll(tc.source, "NAME", field.name)

			t.Run(source, func(t *testing.T) {
				var workload WorkloadMutation
				require.NoError(t, yaml.UnmarshalStrict([]byte(source), &workload))
				require.Equal(t, tc.class, *field.assigned(&workload.Classes))
				require.Equal(t, tc.class != "", workload.HasPodProperties())
				require.Equal(t, tc.class != "", workload.HasProperties())

				copied := workload.DeepCopy()
				require.Equal(t, workload, *copied)

				encoded, err := json.Marshal(copied)
				require.NoError(t, err)

				var decoded WorkloadMutation
				require.NoError(t, json.Unmarshal(encoded, &decoded))
				require.Equal(t, workload, decoded)

				if tc.class == "" {
					require.NotContains(t, string(encoded), "classes")
				}
			})
		}

		var workload WorkloadMutation
		require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"`+field.name+`":"tenant-default"}`), &workload), "unknown field")
		require.Error(t, json.Unmarshal([]byte(`{"classes":{"`+field.name+`":["tenant-default"]}}`), &workload))
	}

	// One entry can assign both classes; each keeps its own value.
	var both WorkloadMutation
	require.NoError(t, yaml.UnmarshalStrict([]byte(`{"classes":{"priority":"gold","runtime":"kata"}}`), &both))
	require.Equal(t, WorkloadClassMutation{Priority: "gold", Runtime: "kata"}, both.Classes)
}
