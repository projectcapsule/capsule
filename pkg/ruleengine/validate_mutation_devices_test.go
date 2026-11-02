// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestDeviceDefaultMutationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, class string
		action      rules.MutationAction
		want        string
	}{
		{"default action", "gpu-default", "", ""},
		{"explicit merge", "gpu.example.com", rules.MutationActionMerge, ""},
		{"template", "{{ .tenant.metadata.name }}-gpu", "", ""},
		{"empty", "", "", "at least one workload mutation property"},
		{"blank", " ", "", ".devices.class"},
		{"invalid name", "bad/name", "", ".devices.class"},
		{"too long", strings.Repeat("a", 254), "", ".devices.class"},
		{"too long template", "{{" + strings.Repeat("a", 4096), "", "4096"},
		{"replacement", "gpu-default", rules.MutationActionReplace, "merge only"},
		{"unknown action", "gpu-default", "other", "unsupported mutation action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: tc.action, Devices: rules.DeviceMutation{Class: tc.class}}}}
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
	// A device default does not make malformed sibling workload properties valid.
	mutation := rules.NamespaceRuleMutation{Devices: rules.DeviceMutation{Class: "gpu"}, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod}}}
	for _, empty := range []bool{false, true} {
		if empty {
			mutation.Devices.Class = ""
		}
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}), "at least one workload mutation property")
	}
}
