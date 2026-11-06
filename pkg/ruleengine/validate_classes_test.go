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

// workloadClassFields builds a rule or a default for each class kind of a workload.
var workloadClassFields = []struct {
	name    string
	enforce func([]rules.WorkloadClassMatch) rules.WorkloadClassEnforcement
	mutate  func(string) rules.WorkloadClassMutation
}{
	{
		name: "priority",
		enforce: func(matches []rules.WorkloadClassMatch) rules.WorkloadClassEnforcement {
			return rules.WorkloadClassEnforcement{Priority: matches}
		},
		mutate: func(name string) rules.WorkloadClassMutation { return rules.WorkloadClassMutation{Priority: name} },
	},
	{
		name: "runtime",
		enforce: func(matches []rules.WorkloadClassMatch) rules.WorkloadClassEnforcement {
			return rules.WorkloadClassEnforcement{Runtime: matches}
		},
		mutate: func(name string) rules.WorkloadClassMutation { return rules.WorkloadClassMutation{Runtime: name} },
	},
}

func TestValidateWorkloadClassRules(t *testing.T) {
	labels := metav1.LabelSelector{MatchLabels: map[string]string{"env": "customer"}}

	for _, field := range workloadClassFields {
		path := "workloads.classes." + field.name

		for _, tc := range []struct {
			name    string
			targets []rules.WorkloadValidationTarget
			classes []rules.WorkloadClassMatch
			want    string
		}{
			{name: "absent"},
			{name: "every matcher", classes: []rules.WorkloadClassMatch{{Exact: []string{"internal.example.com"}, Expression: `^[a-z0-9-]+\.example\.com$`, LabelSelector: labels}}},
			{name: "exact only", classes: []rules.WorkloadClassMatch{{Exact: []string{"gold", "system-cluster-critical"}}}},
			{name: "selector only", classes: []rules.WorkloadClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpExists}}}}},
			{name: "template", classes: []rules.WorkloadClassMatch{{Exact: []string{"{{ .tenant.metadata.name }}-gold"}, MatchLabels: map[string]string{"tenant": "{{ .tenant.metadata.name }}"}}}},
			{name: "controller targets", targets: []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment, rules.ValidateCronJob}, classes: []rules.WorkloadClassMatch{{Exact: []string{"gold"}}}},
			{name: "empty match", classes: []rules.WorkloadClassMatch{{Exact: []string{"gold"}}, {}}, want: "rules[3].enforce." + path + "[1] is invalid: at least one of exact, exp, matchLabels or matchExpressions must be set"},
			{name: "invalid regex", classes: []rules.WorkloadClassMatch{{Expression: "("}}, want: "rules[3].enforce." + path + "[0].exp"},
			{name: "too many exact values", classes: []rules.WorkloadClassMatch{{Exact: make([]string, 65)}}, want: path + "[0].exact: at most 64 values"},
			{name: "invalid label value", classes: []rules.WorkloadClassMatch{{MatchLabels: map[string]string{"env": "invalid!"}}}, want: path + "[0].matchLabels"},
			{name: "invalid operator", classes: []rules.WorkloadClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Unknown"}}}}, want: path + "[0].matchExpressions"},
			{name: "too many classes", classes: make([]rules.WorkloadClassMatch, 65), want: path + ": at most 64 matches are supported"},
			{name: "unsupported target", targets: []rules.WorkloadValidationTarget{"pod/" + rules.WorkloadValidationTarget(field.name) + "class"}, classes: []rules.WorkloadClassMatch{{Exact: []string{"gold"}}}, want: "unsupported workload target"},
		} {
			t.Run(field.name+"/"+tc.name, func(t *testing.T) {
				workloads := rules.NamespaceRuleEnforceWorkloadsBody{Targets: tc.targets, Classes: field.enforce(tc.classes)}
				before := workloads.DeepCopy()

				err := validateWorkloadRules(3, workloads)
				if tc.want == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.want)
				}

				require.Equal(t, *before, workloads)
			})
		}
	}
}

// Storage, priority and runtime classes share one matcher; each keeps its own field path.
func TestClassRulesReportTheirOwnPath(t *testing.T) {
	invalid := []rules.StorageClassMatch{{}}
	valid := []rules.StorageClassMatch{{Exact: []string{"gold"}}}

	for _, field := range workloadClassFields {
		body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: field.enforce(invalid)}}}
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{nil, body}), "rules[1].enforce.workloads.classes."+field.name+"[0] is invalid")
	}

	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Storage: rules.NamespaceRuleEnforceStorageBody{Classes: invalid}}}
	require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{nil, body}), "rules[1].enforce.storage.classes[0] is invalid")

	// A valid list of one kind does not make the other kind's list valid.
	for _, classes := range []rules.WorkloadClassEnforcement{{Priority: valid, Runtime: invalid}, {Priority: invalid, Runtime: valid}} {
		body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: classes}}}
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body}), "[0] is invalid")
	}

	require.NoError(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: valid, Runtime: valid}}}}}))
}

func TestWorkloadClassMutationValidation(t *testing.T) {
	for _, field := range workloadClassFields {
		invalidName := ".workloads.classes." + field.name + ": invalid " + field.name + " class name"

		for _, tc := range []struct {
			name, class string
			action      rules.MutationAction
			targets     []rules.WorkloadValidationTarget
			want        string
		}{
			{name: "default action", class: "tenant-default"},
			{name: "explicit merge", class: "default.example.com", action: rules.MutationActionMerge},
			{name: "replace", class: "tenant-default", action: rules.MutationActionReplace},
			{name: "system class", class: "system-cluster-critical"},
			{name: "template", class: "{{ .tenant.metadata.name }}-default"},
			{name: "pod target", class: "tenant-default", targets: []rules.WorkloadValidationTarget{rules.ValidatePod}},
			{name: "empty", want: "at least one workload mutation property"},
			{name: "blank", class: " ", want: invalidName},
			{name: "invalid name", class: "Bad/Name", want: invalidName},
			{name: "too long", class: strings.Repeat("a", 254), want: invalidName},
			{name: "too long template", class: "{{" + strings.Repeat("a", 4096), want: ".workloads.classes." + field.name + ": at most 4096 characters"},
			{name: "container target only", class: "tenant-default", targets: []rules.WorkloadValidationTarget{rules.ValidateContainers}, want: "Pod-level mutation properties require the pod target"},
			{name: "unknown action", class: "tenant-default", action: "other", want: "unsupported mutation action"},
		} {
			t.Run(field.name+"/"+tc.name, func(t *testing.T) {
				body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: tc.action, Workloads: rules.WorkloadMutation{Targets: tc.targets, Classes: field.mutate(tc.class)}}}}
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

		// A valid class does not make malformed sibling workload properties valid.
		mutation := rules.NamespaceRuleMutation{Workloads: rules.WorkloadMutation{
			Classes:   field.mutate("tenant-default"),
			Placement: rules.WorkloadPlacementMutation{Scheduler: "   "},
		}}
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}), "scheduler name must not be blank")
	}

	// One entry may assign both classes, and each name is validated on its own.
	both := func(priority, runtime string) error {
		mutation := rules.NamespaceRuleMutation{Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: priority, Runtime: runtime}}}

		return ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}})
	}
	require.NoError(t, both("gold", "kata"))
	require.ErrorContains(t, both("gold", "Bad/Name"), "rules[0].mutate[0].workloads.classes.runtime: invalid runtime class name")
	require.ErrorContains(t, both("Bad/Name", "kata"), "rules[0].mutate[0].workloads.classes.priority: invalid priority class name")
}
