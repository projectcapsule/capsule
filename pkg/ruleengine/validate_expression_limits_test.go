// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestValidateExpressionMatchLimitsAcrossRules(t *testing.T) {
	for _, consumer := range []struct {
		path string
		set  func(*rules.NamespaceRuleEnforceBody, apiruntime.ExpressionMatch)
	}{
		{"workloads.registries[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Workloads.Registries = []rules.OCIRegistry{{ExpressionMatch: m}}
		}},
		{"workloads.placement.schedulers[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Workloads.Placement.Schedulers = []apiruntime.ExpressionMatch{m}
		}},
		{"workloads.placement.nodeSelector[0].key", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Workloads.Placement.NodeSelector = []rules.WorkloadNodeSelectorMatch{{Key: (*rules.PlacementExpressionMatch)(&m)}}
		}},
		{"workloads.security.seccompProfiles[0].localhostProfiles[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Workloads.Security.SeccompProfiles = []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{m}}}
		}},
		{"workloads.security.appArmorProfiles[0].localhostProfiles[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Workloads.Security.AppArmorProfiles = []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{m}}}
		}},
		{"ingress.hostnames[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Ingress = rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Hostnames: []apiruntime.ExpressionMatch{m}}
		}},
		{"services.externalNames.hostnames[0]", func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Services = rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeExternalName}, ExternalNames: &rules.ServiceExternalNameRule{Hostnames: []apiruntime.ExpressionMatch{m}}}
		}},
		{`metadata[0].labels["profile"].values[0]`, func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Metadata = []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{Kinds: []string{"Pod"}}, Labels: map[string]rules.MetadataValueRule{"profile": {Values: []apiruntime.ExpressionMatch{m}}}}}
		}},
		{`metadata[0].annotations["profile"].values[0]`, func(b *rules.NamespaceRuleEnforceBody, m apiruntime.ExpressionMatch) {
			b.Metadata = []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{Kinds: []string{"Pod"}}, Annotations: map[string]rules.MetadataValueRule{"profile": {Values: []apiruntime.ExpressionMatch{m}}}}}
		}},
	} {
		for _, tc := range []struct {
			name, field string
			limit       int
			match       func(int) apiruntime.ExpressionMatch
		}{
			{"exact", "exact", 64, func(n int) apiruntime.ExpressionMatch {
				return apiruntime.ExpressionMatch{Exact: slices.Repeat([]string{"allowed"}, n)}
			}},
			{"exp", "exp", 4096, func(n int) apiruntime.ExpressionMatch {
				return apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("a", n)}}
			}},
			{"unicode", "exp", 4096, func(n int) apiruntime.ExpressionMatch {
				return apiruntime.ExpressionMatch{Exact: []string{"allowed"}, ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("界", n), Negate: true}}
			}},
		} {
			for _, n := range []int{tc.limit - 1, tc.limit, tc.limit + 1} {
				t.Run(fmt.Sprintf("%s/%s/%d", consumer.path, tc.name, n), func(t *testing.T) {
					body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow}}
					consumer.set(body.Enforce, tc.match(n))
					before := body.DeepCopy()
					err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
					if n <= tc.limit {
						require.NoError(t, err)
					} else {
						require.ErrorContains(t, err, "rules[0].enforce."+consumer.path+"."+tc.field)
						require.ErrorContains(t, err, fmt.Sprintf("at most %d", tc.limit))
					}
					require.Equal(t, before, body)
				})
			}
		}
	}
}

func BenchmarkValidateExpressionMatchLimits(b *testing.B) {
	for _, count := range []int{1, 10} {
		for _, tc := range []struct {
			name string
			m    apiruntime.ExpressionMatch
			deny bool
		}{
			{"skip", apiruntime.ExpressionMatch{}, false},
			{"exact", apiruntime.ExpressionMatch{Exact: []string{"allowed"}}, false},
			{"exact-limit", apiruntime.ExpressionMatch{Exact: slices.Repeat([]string{"allowed"}, 64)}, false},
			{"exact-oversized", apiruntime.ExpressionMatch{Exact: slices.Repeat([]string{"allowed"}, 65)}, true},
			{"exp", apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: "^allowed$"}}, false},
			{"exp-limit", apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("a", 4096)}}, false},
			{"exp-oversized", apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("a", 4097)}}, true},
		} {
			b.Run(fmt.Sprintf("rules=%d/%s", count, tc.name), func(b *testing.B) {
				bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
				for i := range bodies {
					bodies[i] = &rules.NamespaceRuleBodyNamespace{}
					if tc.name != "skip" {
						bodies[i].Enforce = &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{tc.m}}}}
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					err := ValidateRuleStatusBody(nil, bodies)
					if (err != nil) != tc.deny {
						b.Fatalf("unexpected validation result: %v", err)
					}
				}
			})
		}
	}
}
