// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestCollectStorageClassRegexes(t *testing.T) {
	b := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Storage: rules.NamespaceRuleEnforceStorageBody{
		Targets: []rules.StorageValidationTarget{rules.StorageTargetVolumeSnapshot},
		Classes: []rules.StorageClassMatch{
			{Expression: `^[a-z0-9-]+\.example\.com$`},
			{Exact: []string{"exact-only"}},
			{LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"env": "customer"}}},
		},
	}}}
	set := map[string]apiruntime.ExpressionRegex{}
	collectRegexExpressionsFromNamespaceRules(set, []*rules.NamespaceRuleBodyNamespace{nil, b, b.DeepCopy()})
	require.Len(t, set, 1)
	expr := apiruntime.ExpressionRegex{Expression: `^[a-z0-9-]+\.example\.com$`}
	require.Equal(t, expr, set[cache.HashRegex(expr)])
}
