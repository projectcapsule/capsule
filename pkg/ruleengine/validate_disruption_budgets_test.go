// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateDisruptionBudgetRules(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"valid", `targets: [deployment, pod], disruptionBudgets: {allowOverlap: false, evictableReplicas: {min: 1, max: 2}, unhealthyPodEvictionPolicies: [AlwaysAllow]}`, ""},
		{"pod unhealthy", `targets: [pod], disruptionBudgets: {unhealthyPodEvictionPolicies: [IfHealthyBudget]}`, ""},
		{"negative", `targets: [deployment], disruptionBudgets: {evictableReplicas: {min: -1}}`, "outside"},
		{"inverted", `targets: [statefulset], disruptionBudgets: {evictableReplicas: {min: 2, max: 1}}`, "min must not exceed"},
		{"unsupported target", `targets: [daemonset], disruptionBudgets: {evictableReplicas: {min: 1}}`, "requires a"},
		{"implicit pod", `disruptionBudgets: {evictableReplicas: {min: 1}}`, "requires a"},
		{"invalid policy", `disruptionBudgets: {unhealthyPodEvictionPolicies: [Unknown]}`, "unsupported policy"},
		{"empty", `disruptionBudgets: {}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{}
			require.NoError(t, yaml.UnmarshalStrict([]byte("enforce: {workloads: {"+tc.body+"}}"), body))
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}
