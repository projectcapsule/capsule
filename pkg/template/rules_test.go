// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestRenderNamespaceRuleBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		context  map[string]any
		key      MissingKeyOption
		bodies   []*rules.NamespaceRuleBodyNamespace
		assertFn func(t *testing.T, got []*rules.NamespaceRuleBodyNamespace)
		wantErr  string
	}{
		{
			name: "empty input returns nil",
			key:  MissingKeyOption("error"),
			assertFn: func(t *testing.T, got []*rules.NamespaceRuleBodyNamespace) {
				t.Helper()

				if got != nil {
					t.Fatalf("RenderNamespaceRuleBodies() = %#v, want nil", got)
				}
			},
		},
		{
			name: "renders tenant and namespace values in registry exact match",
			context: map[string]any{
				"tenant": map[string]any{
					"metadata": map[string]any{
						"name": "solar",
					},
				},
				"namespace": map[string]any{
					"metadata": map[string]any{
						"name": "solar-prod",
					},
				},
			},
			key: MissingKeyOption("error"),
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										Exact: []string{
											"{{ .tenant.metadata.name }}/{{ .namespace.metadata.name }}/app:1",
										},
									},
								},
							},
						},
					},
				},
			},
			assertFn: func(t *testing.T, got []*rules.NamespaceRuleBodyNamespace) {
				t.Helper()

				if len(got) != 1 {
					t.Fatalf("len(got) = %d, want 1", len(got))
				}

				body := got[0]
				if body == nil || body.Enforce == nil {
					t.Fatalf("got[0].Enforce = nil, want rendered enforce body")
				}

				if body.Enforce.Action != rules.ActionTypeAllow {
					t.Fatalf("Action = %q, want %q", body.Enforce.Action, rules.ActionTypeAllow)
				}

				registries := body.Enforce.Workloads.Registries
				if len(registries) != 1 {
					t.Fatalf("len(Registries) = %d, want 1", len(registries))
				}

				exact := registries[0].Exact
				if len(exact) != 1 || exact[0] != "solar/solar-prod/app:1" {
					t.Fatalf("Exact = %#v, want [solar/solar-prod/app:1]", exact)
				}
			},
		},
		{
			name: "renders namespace labels using index",
			context: map[string]any{
				"namespace": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"registry-prefix": "harbor/team-a",
						},
					},
				},
			},
			key: MissingKeyOption("error"),
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										Exact: []string{
											`{{ index .namespace.metadata.labels "registry-prefix" }}/app:1`,
										},
									},
								},
							},
						},
					},
				},
			},
			assertFn: func(t *testing.T, got []*rules.NamespaceRuleBodyNamespace) {
				t.Helper()

				exact := got[0].Enforce.Workloads.Registries[0].Exact
				if len(exact) != 1 || exact[0] != "harbor/team-a/app:1" {
					t.Fatalf("Exact = %#v, want [harbor/team-a/app:1]", exact)
				}
			},
		},
		{
			name: "renders multiple bodies while preserving order",
			context: map[string]any{
				"tenant": map[string]any{
					"metadata": map[string]any{
						"name": "solar",
					},
				},
			},
			key: MissingKeyOption("error"),
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										ExpressionRegex: api.ExpressionRegex{
											Expression: "{{ .tenant.metadata.name }}/allow/.*",
										},
									},
								},
							},
						},
					},
				},
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeDeny,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							QoSClasses: []corev1.PodQOSClass{
								corev1.PodQOSBestEffort,
							},
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										ExpressionRegex: api.ExpressionRegex{
											Expression: "{{ .tenant.metadata.name }}/deny/.*",
										},
									},
								},
							},
						},
					},
				},
			},
			assertFn: func(t *testing.T, got []*rules.NamespaceRuleBodyNamespace) {
				t.Helper()

				if len(got) != 2 {
					t.Fatalf("len(got) = %d, want 2", len(got))
				}

				if got[0].Enforce.Action != rules.ActionTypeAllow {
					t.Fatalf("got[0].Action = %q, want allow", got[0].Enforce.Action)
				}

				if got[1].Enforce.Action != rules.ActionTypeDeny {
					t.Fatalf("got[1].Action = %q, want deny", got[1].Enforce.Action)
				}

				first := got[0].Enforce.Workloads.Registries[0].Expression
				if first != "solar/allow/.*" {
					t.Fatalf("first expression = %q, want solar/allow/.*", first)
				}

				second := got[1].Enforce.Workloads.Registries[0].Expression
				if second != "solar/deny/.*" {
					t.Fatalf("second expression = %q, want solar/deny/.*", second)
				}

				qos := got[1].Enforce.Workloads.QoSClasses
				if len(qos) != 1 || qos[0] != corev1.PodQOSBestEffort {
					t.Fatalf("QoSClasses = %#v, want [%q]", qos, corev1.PodQOSBestEffort)
				}
			},
		},
		{
			name: "missing key is wrapped with render namespace rule bodies template",
			context: map[string]any{
				"namespace": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{},
					},
				},
			},
			key: MissingKeyOption("error"),
			bodies: []*rules.NamespaceRuleBodyNamespace{
				{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: api.ExpressionMatch{
										Exact: []string{
											"{{ .namespace.metadata.labels.registry }}/app:1",
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: "render namespace rule bodies template",
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderNamespaceRuleBodies(tt.context, tt.key, tt.bodies)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("RenderNamespaceRuleBodies() error = nil, want containing %q", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RenderNamespaceRuleBodies() error = %q, want containing %q", err.Error(), tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("RenderNamespaceRuleBodies() unexpected error: %v", err)
			}

			if tt.assertFn != nil {
				tt.assertFn(t, got)
			}
		})
	}
}

func TestRenderNamespaceRuleBodies_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	bodies := []*rules.NamespaceRuleBodyNamespace{
		{
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
					Registries: []rules.OCIRegistry{
						{
							ExpressionMatch: api.ExpressionMatch{
								Exact: []string{
									"{{ .tenant.metadata.name }}/app:1",
								},
							},
						},
					},
				},
			},
		},
	}

	got, err := RenderNamespaceRuleBodies(
		map[string]any{
			"tenant": map[string]any{
				"metadata": map[string]any{
					"name": "solar",
				},
			},
		},
		MissingKeyOption("error"),
		bodies,
	)
	if err != nil {
		t.Fatalf("RenderNamespaceRuleBodies() unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	originalExact := bodies[0].Enforce.Workloads.Registries[0].Exact[0]
	if originalExact != "{{ .tenant.metadata.name }}/app:1" {
		t.Fatalf("input body was mutated: exact = %q", originalExact)
	}

	renderedExact := got[0].Enforce.Workloads.Registries[0].Exact[0]
	if renderedExact != "solar/app:1" {
		t.Fatalf("rendered exact = %q, want solar/app:1", renderedExact)
	}
}
