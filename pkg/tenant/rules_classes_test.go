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

func TestPriorityClassNamespaceProfiles(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))

	profile := func(name string, body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": name}}, NamespaceRuleBodyNamespace: body}
	}
	tnt := &capsulev1beta2.Tenant{Name: "a", Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{
		profile("production", &rules.NamespaceRuleBodyNamespace{
			Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: "{{ .tenant.metadata.name }}-production"}}}},
			Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: []rules.WorkloadClassMatch{{
				Exact:       []string{"{{ .tenant.metadata.name }}-production"},
				MatchLabels: map[string]string{"namespace": "{{ .namespace.metadata.name }}"},
			}}}}},
		}),
		profile("batch", &rules.NamespaceRuleBodyNamespace{
			Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: "{{ .tenant.metadata.name }}-batch"}}}},
		}),
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
				require.Equal(t, tenantName+"-batch", bodies[0].Mutate[0].Workloads.Classes.Priority)
			default:
				require.Len(t, bodies, 1)
				require.Equal(t, tenantName+"-production", bodies[0].Mutate[0].Workloads.Classes.Priority)

				match := bodies[0].Enforce.Workloads.Classes.Priority[0]
				require.Equal(t, []string{tenantName + "-production"}, match.Exact)
				require.Equal(t, ns.Name, match.MatchLabels["namespace"])

				// A rendered profile never aliases the Tenant or another namespace.
				match.MatchLabels["namespace"] = "changed"
				match.Exact[0] = "changed"
			}
		}
	}

	tnt.Name = original.Name
	require.Equal(t, original, tnt)
}
