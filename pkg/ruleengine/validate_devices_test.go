// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateDeviceClassRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		matches []rules.DeviceClassMatch
		want    string
	}{
		{name: "omitted"},
		{name: "template name", matches: []rules.DeviceClassMatch{{Exact: []string{"{{ .tenant.metadata.name }}-gpu"}}}},
		{name: "names", matches: []rules.DeviceClassMatch{{Exact: []string{"gpu.example.com"}}}},
		{name: "explicit match all", matches: []rules.DeviceClassMatch{{Selector: &metav1.LabelSelector{}}}},
		{name: "regex", matches: []rules.DeviceClassMatch{{Expression: "^gpu-", Negate: true}}},
		{name: "empty matcher", matches: []rules.DeviceClassMatch{{}}, want: "exact or exp is required"},
		{name: "invalid regex", matches: []rules.DeviceClassMatch{{Expression: "["}}, want: ".exp"},
		{name: "blank regex", matches: []rules.DeviceClassMatch{{Expression: " "}}, want: ".exp must not be blank"},
		{name: "invalid name", matches: []rules.DeviceClassMatch{{Exact: []string{"INVALID/CLASS"}}}, want: ".exact"},
		{name: "empty name", matches: []rules.DeviceClassMatch{{Exact: []string{""}}}, want: ".exact"},
		{name: "too many names", matches: []rules.DeviceClassMatch{{Exact: slices.Repeat([]string{"gpu"}, 65)}}, want: "at most 64"},
		{name: "too long expression", matches: []rules.DeviceClassMatch{{Expression: strings.Repeat("x", 4097)}}, want: "4096"},
		{name: "too many matchers", matches: slices.Repeat([]rules.DeviceClassMatch{{Exact: []string{"gpu"}}}, 65), want: "at most 64"},
		{name: "invalid selector", matches: []rules.DeviceClassMatch{{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tenant", Operator: "Other"}}}}}, want: ".selector"},
		{name: "negate without name", matches: []rules.DeviceClassMatch{{Negate: true, Selector: &metav1.LabelSelector{}}}, want: "exact or exp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: tc.matches}}}
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
				require.ErrorContains(t, err, "rules[0].enforce.devices.classes")
			}
		})
	}
}
