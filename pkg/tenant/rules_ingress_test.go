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
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestListenerSetNamespaceProfiles(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	for _, name := range []string{"tenant-a", "tenant-b"} {
		tnt := &capsulev1beta2.Tenant{Name: name, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "edge"}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: "{{ .tenant.metadata.name }}", Namespace: new("{{ .namespace.metadata.name }}")}}}}}},
		}}}}
		for _, namespace := range []string{"first", "second"} {
			ns := &corev1.Namespace{Name: namespace, Labels: map[string]string{"profile": "edge"}}
			for range 2 {
				bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
				require.NoError(t, err)
				require.Len(t, bodies, 1)
				require.NoError(t, ruleengine.ValidateRuleStatusBody(nil, bodies))
				ref := bodies[0].Mutate[0].Ingress.ListenerSets.ParentRef
				require.Equal(t, name, ref.Name)
				require.Equal(t, namespace, *ref.Namespace)
				ref.Name, *ref.Namespace = "changed", "changed"
			}
			ns.Labels["profile"] = "other"
			bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
			require.NoError(t, err)
			require.Empty(t, bodies)
		}
		tnt.Spec.Rules[0].Mutate[0].Ingress.ListenerSets.ParentRef.Name = "{{ .tenant.metadata.name }}/invalid"
		bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, &corev1.Namespace{Name: "selected", Labels: map[string]string{"profile": "edge"}}, tnt)
		require.NoError(t, err)
		require.ErrorContains(t, ruleengine.ValidateRuleStatusBody(nil, bodies), "parentRef.name")
	}
}
