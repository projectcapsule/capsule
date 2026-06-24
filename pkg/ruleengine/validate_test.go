// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package ruleengine

import (
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateRuleStatusBody(t *testing.T) {
	tests := []struct {
		name    string
		bodies  []*rules.NamespaceRuleBodyNamespace
		wantErr string
	}{
		{
			name: "nil bodies are valid",
			bodies: []*rules.NamespaceRuleBodyNamespace{
				nil,
				{},
				{
					Enforce: nil,
				},
			},
		},
		{
			name: "valid workload and service rules",
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										ExpressionRegex: api.ExpressionRegex{
											Expression: "harbor/.*",
										},
									},
								},
								{
									ExpressionMatch: api.ExpressionMatch{
										Exact: []string{
											"harbor/platform/debian:latest",
										},
									},
								},
							},
							Schedulers: []api.ExpressionMatch{
								{
									ExpressionRegex: api.ExpressionRegex{
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
								Hostnames: []api.ExpressionMatch{
									{
										Exact: []string{
											"internal.git.com",
										},
									},
									{
										ExpressionRegex: api.ExpressionRegex{
											Expression: ".*\\.example\\.com",
										},
									},
									{
										ExpressionRegex: api.ExpressionRegex{
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
			name: "invalid workload registry regex",
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										ExpressionRegex: api.ExpressionRegex{
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
			name: "invalid workload scheduler regex",
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Schedulers: []api.ExpressionMatch{
								{
									ExpressionRegex: api.ExpressionRegex{
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
			name: "invalid service type",
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
			name: "invalid loadBalancer CIDR",
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
			name: "empty loadBalancer CIDR",
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
			name: "invalid externalName hostname regex",
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Services: rules.NamespaceRuleEnforceServicesBody{
							ExternalNames: &rules.ServiceExternalNameRule{
								Hostnames: []api.ExpressionMatch{
									{
										ExpressionRegex: api.ExpressionRegex{
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
			name: "nodePort from greater than to",
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
			wantErr: `rules[0].enforce.services.nodePorts.ports[0] is invalid: from 32767 must be lower than or equal to to 30000`,
		},
		{
			name: "nodePort from below valid port range",
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
			name: "nodePort to above valid port range",
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
			name: "single nodePort range is valid",
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
			name: "reports correct indexes across multiple rules",
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
								Hostnames: []api.ExpressionMatch{
									{
										ExpressionRegex: api.ExpressionRegex{
											Expression: "valid\\..*",
										},
									},
									{
										ExpressionRegex: api.ExpressionRegex{
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
			err := ValidateRuleStatusBody(tt.bodies)

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
