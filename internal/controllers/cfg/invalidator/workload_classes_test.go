// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestCollectWorkloadClassRegexes(t *testing.T) {
	storage := apiruntime.ExpressionRegex{Expression: `^snapshot-[a-z]+$`}
	priority := apiruntime.ExpressionRegex{Expression: `^[a-z0-9-]+\.example\.com$`}
	runtime := apiruntime.ExpressionRegex{Expression: `^(kata|gvisor)-[a-z]+$`}
	b := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Storage: rules.NamespaceRuleEnforceStorageBody{Classes: []rules.StorageClassMatch{{Expression: storage.Expression}}},
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{
			Priority: []rules.WorkloadClassMatch{
				{Expression: priority.Expression},
				{Exact: []string{"exact-only"}},
				{MatchLabels: map[string]string{"env": "customer"}},
			},
			Runtime: []rules.WorkloadClassMatch{
				{Expression: runtime.Expression},
				{Exact: []string{"exact-only"}},
				{MatchLabels: map[string]string{"env": "customer"}},
			},
		}},
	}}
	// A mutation default is a literal name and never reaches the regex cache.
	defaults := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: "tenant-default", Runtime: "tenant-default"}}}}}
	set := map[string]apiruntime.ExpressionRegex{}
	collectRegexExpressionsFromNamespaceRules(set, []*rules.NamespaceRuleBodyNamespace{nil, defaults, b, b.DeepCopy()})
	require.Len(t, set, 3)
	require.Equal(t, storage, set[cache.HashRegex(storage)])
	require.Equal(t, priority, set[cache.HashRegex(priority)])
	require.Equal(t, runtime, set[cache.HashRegex(runtime)])

	// A rule that only matches runtime classes keeps its expression warm too.
	set = map[string]apiruntime.ExpressionRegex{}
	collectRegexExpressionsFromNamespaceRules(set, []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Runtime: []rules.WorkloadClassMatch{{Expression: runtime.Expression}}}},
	}}})
	require.Equal(t, map[string]apiruntime.ExpressionRegex{cache.HashRegex(runtime): runtime}, set)
}
