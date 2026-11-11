// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestAttributeClassDefaultMutationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, class string
		action      rules.MutationAction
		want        string
	}{
		{"default action", "gold", "", ""},
		{"explicit merge", "gold.example.com", rules.MutationActionMerge, ""},
		{"replace", "gold", rules.MutationActionReplace, ""},
		{"template", "{{ .tenant.metadata.name }}-gold", "", ""},
		{"empty", "", "", "at least one workload mutation property"},
		{"blank", " ", "", ".storage.attributeClass: invalid volume attributes class name"},
		{"invalid name", "bad/name", "", ".storage.attributeClass: invalid volume attributes class name"},
		{"uppercase", "Gold", "", ".storage.attributeClass: invalid volume attributes class name"},
		{"too long", strings.Repeat("a", 254), "", ".storage.attributeClass"},
		{"too long template", "{{" + strings.Repeat("a", 4096), "", "4096"},
		{"unknown action", "gold", "other", "unsupported mutation action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: tc.action, Storage: rules.StorageMutation{AttributeClass: tc.class}}}}
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

	t.Run("a class default does not make malformed sibling workload properties valid", func(t *testing.T) {
		mutation := rules.NamespaceRuleMutation{Storage: rules.StorageMutation{AttributeClass: "gold"}, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod}}}
		require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}), "at least one workload mutation property")
	})

	t.Run("next to other defaults", func(t *testing.T) {
		mutation := rules.NamespaceRuleMutation{Storage: rules.StorageMutation{AttributeClass: "gold"}, Devices: rules.DeviceMutation{Class: "gpu"}}
		require.NoError(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{mutation}}}))
	})
}
