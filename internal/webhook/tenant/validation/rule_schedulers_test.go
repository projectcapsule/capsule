// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestLegacySchedulerRuleValidation(t *testing.T) {
	h := RuleHandler(nil, nil)
	for _, expression := range []string{"", "^team-", "["} {
		t.Run(expression, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
						Schedulers: []runtime.ExpressionMatch{{ExpressionRegex: runtime.ExpressionRegex{Expression: expression}}},
						Placement:  rules.WorkloadPlacementEnforcement{Schedulers: []runtime.ExpressionMatch{{Exact: []string{"preferred"}}}},
					},
				}},
			}}}}
			before := tnt.DeepCopy()
			for _, response := range []*admission.Response{
				h.OnCreate(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}),
				h.OnUpdate(nil, nil, tnt, before, nil, nil)(t.Context(), admission.Request{}),
			} {
				if expression == "[" {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, `rules[0].enforce.workloads.schedulers[0].exp "[" is invalid`)
				} else {
					require.Nil(t, response)
				}
			}
			require.Equal(t, before, tnt)
		})
	}
}
