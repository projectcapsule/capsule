// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestValidateSecurityProfiles(t *testing.T) {
	valid := &rules.NamespaceRuleBodyNamespace{
		Mutate:  []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: new("team-profile")}}}},
		Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{SeccompProfiles: []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileRuntimeDefault, rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{{ExpressionRegex: apiruntime.ExpressionRegex{Expression: `^teams/[^/]+\.json$`}}}}}}},
	}
	require.NoError(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{valid}))
	for _, tc := range []struct {
		name, want string
		change     func(*rules.NamespaceRuleBodyNamespace)
	}{
		{"missing type", "seccompProfile.type", func(b *rules.NamespaceRuleBodyNamespace) { b.Mutate[0].Workloads.SeccompProfile.Type = "" }},
		{"unknown type", "seccompProfile.type", func(b *rules.NamespaceRuleBodyNamespace) { b.Mutate[0].Workloads.SeccompProfile.Type = "Other" }},
		{"missing local file", "seccompProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.SeccompProfile.Type = corev1.SeccompProfileTypeLocalhost
		}},
		{"unexpected local file", "seccompProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.SeccompProfile.LocalhostProfile = new("a.json")
		}},
		{"absolute path", "seccompProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: new("/a.json")}
		}},
		{"path traversal", "seccompProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: new("a/../b.json")}
		}},
		{"blank AppArmor", "appArmorProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.AppArmorProfile.LocalhostProfile = new(" ")
		}},
		{"unexpected AppArmor name", "appArmorProfile.localhostProfile", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Mutate[0].Workloads.AppArmorProfile.Type = corev1.AppArmorProfileTypeUnconfined
		}},
		{"no matched types", "seccompProfiles[0].types", func(b *rules.NamespaceRuleBodyNamespace) { b.Enforce.Workloads.SeccompProfiles[0].Types = nil }},
		{"invalid matched type", "seccompProfiles[0].types", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Enforce.Workloads.SeccompProfiles[0].Types = []rules.SecurityProfileType{"Other"}
		}},
		{"name without Localhost", "localhostProfiles", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Enforce.Workloads.SeccompProfiles[0].Types = []rules.SecurityProfileType{rules.SecurityProfileRuntimeDefault}
		}},
		{"invalid regex", "localhostProfiles[0].exp", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Enforce.Workloads.SeccompProfiles[0].LocalhostProfiles[0].Expression = "["
		}},
		{"empty matcher", "localhostProfiles[0]", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Enforce.Workloads.SeccompProfiles[0].LocalhostProfiles[0] = apiruntime.ExpressionMatch{}
		}},
		{"empty exact", "localhostProfiles[0].exact", func(b *rules.NamespaceRuleBodyNamespace) {
			b.Enforce.Workloads.SeccompProfiles[0].LocalhostProfiles[0] = apiruntime.ExpressionMatch{Exact: []string{""}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := valid.DeepCopy()
			tc.change(body)
			require.ErrorContains(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body}), tc.want)
		})
	}
	for _, p := range []string{"profiles/tenant.json", "profiles/{{ .tenant.metadata.name }}.json"} {
		body := valid.DeepCopy()
		body.Mutate[0].Workloads.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: new(p)}
		require.NoError(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body}))
	}
	for _, target := range []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment} {
		w := valid.Enforce.Workloads
		w.Targets = []rules.WorkloadValidationTarget{target}
		require.True(t, w.HasPolicies())
		require.False(t, w.TargetsOnly())
	}
}
