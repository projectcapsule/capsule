// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"fmt"
	"strings"
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

const gatewayNamespaceTemplate = `{{ index .tenant.metadata.labels "networking.example.com/default-gateway-namespace" }}`

func TestListenerSetLongNamespaceTemplates(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	require.Greater(t, len(gatewayNamespaceTemplate), 63)
	for _, tc := range []struct {
		name, namespace string
		valid           bool
	}{
		{"valid", "edge", true},
		{"maximum length", strings.Repeat("a", 63), true},
		{"too long", strings.Repeat("a", 64), false},
		{"invalid DNS label", "invalid.namespace", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", Labels: map[string]string{"networking.example.com/default-gateway-namespace": tc.namespace}, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: "edge", Namespace: new(gatewayNamespaceTemplate)}}}}}},
			}}}}
			before := tnt.DeepCopy()
			require.NoError(t, ruleengine.ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{tnt.Spec.Rules[0].NamespaceRuleBodyNamespace}))
			bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, &corev1.Namespace{Name: "selected"}, tnt)
			require.NoError(t, err)
			require.Len(t, bodies, 1)
			require.Equal(t, tc.namespace, *bodies[0].Mutate[0].Ingress.ListenerSets.ParentRef.Namespace)
			err = ruleengine.ValidateRuleStatusBody(nil, bodies)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "parentRef.namespace")
			}
			require.Equal(t, before, tnt)
		})
	}
}

func BenchmarkListenerSetNamespaceTemplateProjection(b *testing.B) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	for _, tenantCount := range []int{1, 16} {
		for _, ruleCount := range []int{1, 32} {
			for _, sourceLength := range []int{len(gatewayNamespaceTemplate), 4096} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/source=%d", tenantCount, ruleCount, sourceLength), func(b *testing.B) {
					template := strings.Replace(gatewayNamespaceTemplate, "{{", "{{"+strings.Repeat(" ", sourceLength-len(gatewayNamespaceTemplate)), 1)
					tenants := make([]*capsulev1beta2.Tenant, tenantCount)
					for i := range tenants {
						tnt := &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i), Labels: map[string]string{"networking.example.com/default-gateway-namespace": fmt.Sprintf("edge-%d", i)}}
						for range ruleCount {
							tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: "edge", Namespace: new(template)}}}}}}})
						}
						tenants[i] = tnt
					}
					ns := &corev1.Namespace{Name: "selected"}
					iteration := 0
					b.ReportAllocs()
					for b.Loop() {
						index := iteration % tenantCount
						bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tenants[index])
						if err != nil {
							b.Fatal(err)
						}
						if len(bodies) != ruleCount || *bodies[0].Mutate[0].Ingress.ListenerSets.ParentRef.Namespace != tenants[index].Labels["networking.example.com/default-gateway-namespace"] {
							b.Fatal("incorrect namespace profile")
						}
						iteration++
					}
				})
			}
		}
	}
}

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
