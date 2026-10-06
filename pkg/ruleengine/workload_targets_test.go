// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestWorkloadEnforcementScope(t *testing.T) {
	pod := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
	deployment := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	for _, tc := range []struct {
		name          string
		targets, want []rules.WorkloadValidationTarget
		gvk           schema.GroupVersionKind
		selected      bool
	}{
		{"default Pod", nil, nil, pod, true},
		{"default skips controller", nil, nil, deployment, false},
		{"Pod keeps container scope", []rules.WorkloadValidationTarget{rules.ValidateContainers}, []rules.WorkloadValidationTarget{rules.ValidateContainers}, pod, true},
		{"controller does not leak to Pod", []rules.WorkloadValidationTarget{rules.ValidateDeployment}, nil, pod, false},
		{"whole template", []rules.WorkloadValidationTarget{rules.ValidateDeployment}, nil, deployment, true},
		{"template containers", []rules.WorkloadValidationTarget{"deployment/containers"}, []rules.WorkloadValidationTarget{rules.ValidateContainers}, deployment, true},
		{"mixed workloads", []rules.WorkloadValidationTarget{rules.ValidatePod, "deployment/initcontainers"}, []rules.WorkloadValidationTarget{rules.ValidateInitContainers}, deployment, true},
		{"foreign group", []rules.WorkloadValidationTarget{rules.ValidateDeployment}, nil, schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Deployment"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: tc.targets, Placement: rules.WorkloadPlacementEnforcement{Schedulers: []runtime.ExpressionMatch{{Exact: []string{"batch"}}}}}}
			before := body.DeepCopy()
			got := WorkloadEnforcement([]*rules.NamespaceRuleEnforceBody{body}, tc.gvk)
			if tc.selected {
				require.Len(t, got, 1)
				require.Equal(t, tc.want, got[0].Workloads.Targets)
			} else {
				require.Empty(t, got)
			}
			require.Equal(t, before, body, "normalizing targets must not mutate shared rules")
			copy := body.DeepCopy()
			if len(copy.Workloads.Targets) > 0 {
				copy.Workloads.Targets[0] = "changed"
				require.Equal(t, before, body)
			}
		})
	}
	// Kind-only rules must not become an accidental allow-list for Pod properties.
	require.Empty(t, WorkloadEnforcement([]*rules.NamespaceRuleEnforceBody{{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod}}}}, pod))
}
