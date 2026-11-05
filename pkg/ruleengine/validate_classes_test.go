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

func TestValidateWorkloadPriorityClassRules(t *testing.T) {
	labels := metav1.LabelSelector{MatchLabels: map[string]string{"env": "customer"}}

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
		{name: "empty match", classes: []rules.WorkloadClassMatch{{Exact: []string{"gold"}}, {}}, want: "rules[3].enforce.workloads.classes.priority[1] is invalid: at least one of exact, exp, matchLabels or matchExpressions must be set"},
		{name: "invalid regex", classes: []rules.WorkloadClassMatch{{Expression: "("}}, want: "rules[3].enforce.workloads.classes.priority[0].exp"},
		{name: "too many exact values", classes: []rules.WorkloadClassMatch{{Exact: make([]string, 65)}}, want: "workloads.classes.priority[0].exact: at most 64 values"},
		{name: "invalid label value", classes: []rules.WorkloadClassMatch{{MatchLabels: map[string]string{"env": "invalid!"}}}, want: "workloads.classes.priority[0].matchLabels"},
		{name: "invalid operator", classes: []rules.WorkloadClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Unknown"}}}}, want: "workloads.classes.priority[0].matchExpressions"},
		{name: "too many classes", classes: make([]rules.WorkloadClassMatch, 65), want: "workloads.classes.priority: at most 64 matches are supported"},
		{name: "unsupported target", targets: []rules.WorkloadValidationTarget{"pod/priorityclass"}, classes: []rules.WorkloadClassMatch{{Exact: []string{"gold"}}}, want: "unsupported workload target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workloads := rules.NamespaceRuleEnforceWorkloadsBody{Targets: tc.targets, Classes: rules.WorkloadClassEnforcement{Priority: tc.classes}}
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

// Storage and priority classes share one matcher; each keeps its own field path.
func TestClassRulesReportTheirOwnPath(t *testing.T) {
	invalid := []rules.StorageClassMatch{{}}
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: invalid}}}}
	require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{nil, body}), "rules[1].enforce.workloads.classes.priority[0] is invalid")

	body = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Storage: rules.NamespaceRuleEnforceStorageBody{Classes: invalid}}}
	require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{nil, body}), "rules[1].enforce.storage.classes[0] is invalid")
}

func TestPriorityClassMutationValidation(t *testing.T) {
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
		{name: "blank", class: " ", want: ".workloads.classes.priority: invalid priority class name"},
		{name: "invalid name", class: "Bad/Name", want: ".workloads.classes.priority: invalid priority class name"},
		{name: "too long", class: strings.Repeat("a", 254), want: ".workloads.classes.priority: invalid priority class name"},
		{name: "too long template", class: "{{" + strings.Repeat("a", 4096), want: ".workloads.classes.priority: at most 4096 characters"},
		{name: "container target only", class: "tenant-default", targets: []rules.WorkloadValidationTarget{rules.ValidateContainers}, want: "Pod-level mutation properties require the pod target"},
		{name: "unknown action", class: "tenant-default", action: "other", want: "unsupported mutation action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: tc.action, Workloads: rules.WorkloadMutation{Targets: tc.targets, Classes: rules.WorkloadClassMutation{Priority: tc.class}}}}}
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
		Classes:   rules.WorkloadClassMutation{Priority: "tenant-default"},
		Placement: rules.WorkloadPlacementMutation{Scheduler: "   "},
	}}
	require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}), "scheduler name must not be blank")
}
