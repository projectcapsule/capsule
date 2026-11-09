// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// Every class kind of a workload is rendered per namespace the same way.
func TestWorkloadClassNamespaceProfiles(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))

	for _, class := range []struct {
		name     string
		matches  func(*rules.WorkloadClassEnforcement) *[]rules.WorkloadClassMatch
		assigned func(*rules.WorkloadClassMutation) *string
	}{
		{name: "priority", matches: func(c *rules.WorkloadClassEnforcement) *[]rules.WorkloadClassMatch { return &c.Priority }, assigned: func(c *rules.WorkloadClassMutation) *string { return &c.Priority }},
		{name: "runtime", matches: func(c *rules.WorkloadClassEnforcement) *[]rules.WorkloadClassMatch { return &c.Runtime }, assigned: func(c *rules.WorkloadClassMutation) *string { return &c.Runtime }},
	} {
		t.Run(class.name, func(t *testing.T) {
			defaulted := func(action rules.MutationAction, name string) []rules.NamespaceRuleMutation {
				mutation := rules.NamespaceRuleMutation{Action: action}
				*class.assigned(&mutation.Workloads.Classes) = name

				return []rules.NamespaceRuleMutation{mutation}
			}
			profile := func(name string, body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyTenant {
				return &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": name}}, NamespaceRuleBodyNamespace: body}
			}

			enforce := &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow}
			*class.matches(&enforce.Workloads.Classes) = []rules.WorkloadClassMatch{{
				Exact:       []string{"{{ .tenant.metadata.name }}-production"},
				MatchLabels: map[string]string{"namespace": "{{ .namespace.metadata.name }}"},
			}}

			tnt := &capsulev1beta2.Tenant{Name: "a", Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{
				profile("production", &rules.NamespaceRuleBodyNamespace{Mutate: defaulted("", "{{ .tenant.metadata.name }}-production"), Enforce: enforce}),
				profile("batch", &rules.NamespaceRuleBodyNamespace{Mutate: defaulted(rules.MutationActionReplace, "{{ .tenant.metadata.name }}-batch")}),
			}}}
			original := tnt.DeepCopy()

			for _, tenantName := range []string{"a", "b"} {
				tnt.Name = tenantName

				for _, profile := range []string{"production", "batch", "other"} {
					ns := &corev1.Namespace{Name: tenantName + "-" + profile, Labels: map[string]string{"profile": profile}}

					bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
					require.NoError(t, err)

					switch profile {
					case "other":
						require.Empty(t, bodies, "non-selected namespaces receive no class rules")
					case "batch":
						require.Len(t, bodies, 1)
						require.Nil(t, bodies[0].Enforce)
						require.Equal(t, rules.MutationActionReplace, bodies[0].Mutate[0].Action)
						require.Equal(t, tenantName+"-batch", *class.assigned(&bodies[0].Mutate[0].Workloads.Classes))
					default:
						require.Len(t, bodies, 1)
						require.Equal(t, tenantName+"-production", *class.assigned(&bodies[0].Mutate[0].Workloads.Classes))

						match := (*class.matches(&bodies[0].Enforce.Workloads.Classes))[0]
						require.Equal(t, []string{tenantName + "-production"}, match.Exact)
						require.Equal(t, ns.Name, match.MatchLabels["namespace"])

						// A rendered profile never aliases the Tenant or another namespace.
						match.MatchLabels["namespace"] = "changed"
						match.Exact[0] = "changed"
					}

					// A profile of one class kind never sets a rule or a default of the other.
					for _, body := range bodies {
						classes := rules.WorkloadClassMutation{}
						*class.assigned(&classes) = *class.assigned(&body.Mutate[0].Workloads.Classes)
						require.Equal(t, classes, body.Mutate[0].Workloads.Classes)
					}
				}
			}

			tnt.Name = original.Name
			require.Equal(t, original, tnt)
		})
	}
}
