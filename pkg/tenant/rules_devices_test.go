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

func TestDeviceClassNamespaceProfiles(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	tnt := &capsulev1beta2.Tenant{Name: "a", Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
		NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "gpu"}},
		NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Devices: rules.DeviceMutation{Class: "{{ .tenant.metadata.name }}-gpu"}}}, Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: []rules.DeviceClassMatch{{Exact: []string{"{{ .tenant.metadata.name }}-gpu"}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"namespace": "{{ .namespace.metadata.name }}"}}}}}}},
	}}}}
	original := tnt.DeepCopy()
	for _, tenantName := range []string{"a", "b"} {
		tnt.Name = tenantName
		for _, profile := range []string{"gpu", "other"} {
			ns := &corev1.Namespace{Name: tenantName + "-ns", Labels: map[string]string{"profile": profile}}
			bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
			require.NoError(t, err)
			if profile == "other" {
				require.Empty(t, bodies)
				continue
			}
			require.Len(t, bodies, 1)
			require.Equal(t, tenantName+"-gpu", bodies[0].Mutate[0].Devices.Class)
			require.Equal(t, tenantName+"-gpu", bodies[0].Enforce.Devices.Classes[0].Exact[0])
			require.Equal(t, ns.Name, bodies[0].Enforce.Devices.Classes[0].Selector.MatchLabels["namespace"])
			bodies[0].Enforce.Devices.Classes[0].Selector.MatchLabels["namespace"] = "changed"
		}
	}
	tnt.Name = original.Name
	require.Equal(t, original, tnt)
}
