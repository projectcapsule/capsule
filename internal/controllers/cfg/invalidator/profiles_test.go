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

func TestCollectSecurityProfileRegexes(t *testing.T) {
	match := rules.WorkloadSecurityProfileMatch{Types: []rules.SecurityProfileType{rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{{ExpressionRegex: apiruntime.ExpressionRegex{Expression: "^tenant-a/"}}, {Exact: []string{"exact-only"}}}}
	b := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Security: rules.WorkloadSecurityEnforcement{SeccompProfiles: []rules.WorkloadSecurityProfileMatch{match}, AppArmorProfiles: []rules.WorkloadSecurityProfileMatch{match}}}}}
	set := map[string]apiruntime.ExpressionRegex{}
	collectRegexExpressionsFromNamespaceRules(set, []*rules.NamespaceRuleBodyNamespace{nil, b, b.DeepCopy()})
	require.Len(t, set, 1)
	expr := match.LocalhostProfiles[0].ExpressionRegex
	require.Equal(t, expr, set[cache.HashRegex(expr)])
}
