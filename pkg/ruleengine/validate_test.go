// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestValidateRuleStatusBody(t *testing.T) {
	t.Parallel()

	mapper := newRuleValidationRESTMapper()

	tests := []struct {
		name    string
		bodies  []*rules.NamespaceRuleBodyNamespace
		mapper  apimeta.RESTMapper
		wantErr string
	}{
		{
			name:   "nil bodies are valid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				nil,
				{},
				{
					Enforce: nil,
				},
			},
		},
		{
			name:   "valid workload service and metadata rules",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
										"Service",
										"Deployment",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "^(prod|test|dev)$",
												},
											},
											{
												Exact: []string{
													"prod",
													"test",
												},
											},
										},
									},
									"presence-only": {
										Required: true,
									},
								},
								Annotations: map[string]rules.MetadataValueRule{
									"example.corp/cost-center": {
										Required: false,
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "^INV-[0-9]{4}$",
												},
											},
											{
												Exact: []string{
													"prod",
													"test",
												},
											},
										},
									},
								},
							},
						},
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: runtime.ExpressionMatch{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "harbor/.*",
										},
									},
								},
								{
									ExpressionMatch: runtime.ExpressionMatch{
										Exact: []string{
											"harbor/platform/debian:latest",
										},
									},
								},
							},
							Schedulers: []runtime.ExpressionMatch{
								{
									ExpressionRegex: runtime.ExpressionRegex{
										Expression: "tenant-[a-z0-9-]+",
									},
								},
							},
						},
						Services: rules.NamespaceRuleEnforceServicesBody{
							Types: []rules.ServiceType{
								rules.ServiceTypeClusterIP,
								rules.ServiceTypeNodePort,
								rules.ServiceTypeLoadBalancer,
								rules.ServiceTypeExternalName,
							},
							LoadBalancers: &rules.ServiceLoadBalancerRule{
								CIDRs: []string{
									"10.0.0.2/32",
									"10.0.1.0/24",
									"2001:db8::/32",
									"10.0.0.3",
								},
							},
							ExternalNames: &rules.ServiceExternalNameRule{
								Hostnames: []runtime.ExpressionMatch{
									{
										Exact: []string{
											"internal.git.com",
										},
									},
									{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: ".*\\.example\\.com",
										},
									},
									{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "trusted\\..*",
											Negate:     true,
										},
									},
								},
							},
							NodePorts: &rules.ServiceNodePortRule{
								Ports: []rules.ServiceNodePortRange{
									{
										From: 30000,
										To:   32767,
									},
									{
										From: 30500,
										To:   30500,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "valid metadata rule with empty apiVersion meaning core v1",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"ConfigMap",
										"Service",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"prod",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "valid metadata rule with wildcard apiVersion and wildcard kind",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAudit,
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"*",
									},
								},
								Annotations: map[string]rules.MetadataValueRule{
									"example.corp/audit": {
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "^audit-.*",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "valid metadata rule with partial wildcards",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"apps/*",
									},
									Kinds: []string{
										"*Set",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"prod",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "invalid metadata label regex",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "[",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.metadata[0].labels["env"].values[0].exp "[" is invalid`,
		},
		{
			name:   "invalid metadata annotation regex",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Annotations: map[string]rules.MetadataValueRule{
									"example.corp/cost-center": {
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "[",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.metadata[0].annotations["example.corp/cost-center"].values[0].exp "[" is invalid`,
		},
		{
			name:   "invalid metadata label key",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"bad/key/again": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"prod",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.metadata[0].labels["bad/key/again"] is invalid`,
		},
		{
			name:   "invalid metadata annotation key",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Annotations: map[string]rules.MetadataValueRule{
									"bad/key/again": {
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"value",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.metadata[0].annotations["bad/key/again"] is invalid`,
		},
		{
			name:   "reports correct metadata indexes across multiple rules and values",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "valid-.*",
												},
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"Service",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"team": {
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"platform",
												},
											},
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "[",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[1].enforce.metadata[0].labels["team"].values[1].exp "[" is invalid`,
		},
		{
			name:   "invalid workload registry regex",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: runtime.ExpressionMatch{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "[",
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.workloads.registries[0].exp "[" is invalid`,
		},
		{
			name:   "invalid workload scheduler regex",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Schedulers: []runtime.ExpressionMatch{
								{
									ExpressionRegex: runtime.ExpressionRegex{
										Expression: "[",
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.workloads.schedulers[0].exp "[" is invalid`,
		},
		{
			name:   "invalid service type",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							Types: []rules.ServiceType{
								rules.ServiceTypeClusterIP,
								rules.ServiceType("InvalidType"),
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.types[1] "InvalidType" is invalid`,
		},
		{
			name:   "invalid loadBalancer CIDR",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							LoadBalancers: &rules.ServiceLoadBalancerRule{
								CIDRs: []string{
									"10.0.0.0/33",
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.loadBalancers.cidrs[0] "10.0.0.0/33" is invalid`,
		},
		{
			name:   "empty loadBalancer CIDR",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							LoadBalancers: &rules.ServiceLoadBalancerRule{
								CIDRs: []string{
									"",
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.loadBalancers.cidrs[0] "" is invalid: CIDR is empty`,
		},
		{
			name:   "invalid externalName hostname regex",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							ExternalNames: &rules.ServiceExternalNameRule{
								Hostnames: []runtime.ExpressionMatch{
									{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "[",
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.externalNames.hostnames[0].exp "[" is invalid`,
		},
		{
			name:   "nodePort from greater than to",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							NodePorts: &rules.ServiceNodePortRule{
								Ports: []rules.ServiceNodePortRange{
									{
										From: 32767,
										To:   30000,
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.nodePorts.ports[0] is invalid: from 32767 must be lower than or equal to 30000`,
		},
		{
			name:   "nodePort from below valid port range",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							NodePorts: &rules.ServiceNodePortRule{
								Ports: []rules.ServiceNodePortRange{
									{
										From: 0,
										To:   30000,
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.nodePorts.ports[0] is invalid: from 0 must be between 1 and 65535`,
		},
		{
			name:   "nodePort to above valid port range",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							NodePorts: &rules.ServiceNodePortRule{
								Ports: []rules.ServiceNodePortRange{
									{
										From: 30000,
										To:   70000,
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[0].enforce.services.nodePorts.ports[0] is invalid: to 70000 must be between 1 and 65535`,
		},
		{
			name:   "single nodePort range is valid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							NodePorts: &rules.ServiceNodePortRule{
								Ports: []rules.ServiceNodePortRange{
									{
										From: 30500,
										To:   30500,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "reports correct indexes across multiple service rules",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							Types: []rules.ServiceType{
								rules.ServiceTypeClusterIP,
							},
						},
					},
				},
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							ExternalNames: &rules.ServiceExternalNameRule{
								Hostnames: []runtime.ExpressionMatch{
									{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "valid\\..*",
										},
									},
									{
										ExpressionRegex: runtime.ExpressionRegex{
											Expression: "[",
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: `rules[1].enforce.services.externalNames.hostnames[1].exp "[" is invalid`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleStatusBody(tt.mapper, tt.bodies)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestValidateRuleStatusBodyWithRESTMapper(t *testing.T) {
	t.Parallel()

	mapper := newRuleValidationRESTMapper()

	tests := []struct {
		name    string
		bodies  []*rules.NamespaceRuleBodyNamespace
		mapper  apimeta.RESTMapper
		wantErr []string
	}{
		{
			name:   "nil mapper skips discovery validation",
			mapper: nil,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"NotAThing",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "known core v1 multiple kinds are valid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"ConfigMap",
										"Service",
										"Pod",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												Exact: []string{
													"prod",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "known grouped apiVersion kind is valid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"apps/v1",
									},
									Kinds: []string{
										"Deployment",
										"StatefulSet",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "unknown core kind is invalid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"NotAThing",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0]`,
				`NotAThing`,
			},
		},
		{
			name:   "wrong apiVersion kind combination is invalid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"Deployment",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0]`,
				`Deployment`,
			},
		},
		{
			name:   "unknown grouped kind is invalid",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"apps/v1",
									},
									Kinds: []string{
										"NotADeployment",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
			wantErr: []string{
				`rules[0].enforce.metadata[0].kinds[0]`,
				`NotADeployment`,
			},
		},
		{
			name:   "wildcard apiVersion skips discovery validation",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"*",
									},
									Kinds: []string{
										"NotAThing",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "wildcard kind skips discovery validation",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"*",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "partial wildcard kind skips discovery validation",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"apps/v1",
									},
									Kinds: []string{
										"*Set",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "reports correct indexes across multiple metadata rules and kinds",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
									},
								},
							},
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"apps/v1",
									},
									Kinds: []string{
										"Deployment",
										"NotADeployment",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"team": {
										Required: true,
									},
								},
							},
						},
					},
				},
			},
			wantErr: []string{
				`rules[0].enforce.metadata[1].kinds[1]`,
				`NotADeployment`,
			},
		},
		{
			name:   "still validates metadata syntax when mapper is enabled",
			mapper: mapper,
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{
							{
								VersionKinds: runtime.VersionKinds{
									APIGroups: []string{
										"v1",
									},
									Kinds: []string{
										"ConfigMap",
									},
								},
								Labels: map[string]rules.MetadataValueRule{
									"env": {
										Required: true,
										Values: []runtime.ExpressionMatch{
											{
												ExpressionRegex: runtime.ExpressionRegex{
													Expression: "[",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: []string{
				`rules[0].enforce.metadata[0].labels["env"].values[0].exp "[" is invalid`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleStatusBody(tt.mapper, tt.bodies)

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}

			for _, expected := range tt.wantErr {
				if !strings.Contains(err.Error(), expected) {
					t.Fatalf("expected error containing %q, got %q", expected, err.Error())
				}
			}
		})
	}
}

func newRuleValidationRESTMapper() apimeta.RESTMapper {
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{
		{
			Group:   "",
			Version: "v1",
		},
		{
			Group:   "apps",
			Version: "v1",
		},
	})

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Service",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		},
		apimeta.RESTScopeNamespace,
	)

	mapper.Add(
		schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "StatefulSet",
		},
		apimeta.RESTScopeNamespace,
	)

	return mapper
}

func TestValidateFieldRulesBody(t *testing.T) {
	t.Parallel()

	mapper := newRuleValidationRESTMapper()

	validMatch := []runtime.ExpressionMatch{
		{
			Exact: []string{"fast-ssd"},
		},
	}

	makeBody := func(rule rules.FieldRule) []*rules.NamespaceRuleBodyNamespace {
		return []*rules.NamespaceRuleBodyNamespace{
			{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: rules.ActionTypeAllow,
					Fields: []rules.FieldRule{rule},
				},
			},
		}
	}

	tests := []struct {
		name    string
		rule    rules.FieldRule
		wantErr string
	}{
		{
			name: "valid field rule",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"apps"},
					Kinds:     []string{"Deployment"},
				},
				Path:  ".spec.template.spec.containers[*].image",
				Match: validMatch,
			},
		},
		{
			name: "wildcard kinds are valid",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"*"},
					Kinds:     []string{"*"},
				},
				Path:  ".spec.storageClassName",
				Match: validMatch,
			},
		},
		{
			name: "missing kinds",
			rule: rules.FieldRule{
				Path:  ".spec.storageClassName",
				Match: validMatch,
			},
			wantErr: "rules[0].enforce.fields[0].kinds is invalid",
		},
		{
			name: "unknown kind",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"apps"},
					Kinds:     []string{"DoesNotExist"},
				},
				Path:  ".spec.storageClassName",
				Match: validMatch,
			},
			wantErr: "rules[0].enforce.fields[0].kinds[0]",
		},
		{
			name: "invalid path",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"apps"},
					Kinds:     []string{"Deployment"},
				},
				Path:  ".spec.containers[",
				Match: validMatch,
			},
			wantErr: "rules[0].enforce.fields[0].path",
		},
		{
			name: "empty match",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"apps"},
					Kinds:     []string{"Deployment"},
				},
				Path: ".spec.storageClassName",
			},
			wantErr: "rules[0].enforce.fields[0].match is invalid",
		},
		{
			name: "invalid match expression",
			rule: rules.FieldRule{
				VersionKinds: runtime.VersionKinds{
					APIGroups: []string{"apps"},
					Kinds:     []string{"Deployment"},
				},
				Path: ".spec.storageClassName",
				Match: []runtime.ExpressionMatch{
					{
						ExpressionRegex: runtime.ExpressionRegex{
							Expression: "([",
						},
					},
				},
			},
			wantErr: "rules[0].enforce.fields[0].match[0].exp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleStatusBody(mapper, makeBody(tt.rule))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
