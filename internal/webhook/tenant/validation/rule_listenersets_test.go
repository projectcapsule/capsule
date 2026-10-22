// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestListenerSetNamespaceTemplateValidation(t *testing.T) {
	h := RuleHandler(nil, nil)
	for _, namespace := range []string{
		`{{ index .tenant.metadata.labels "networking.example.com/default-gateway-namespace" }}`,
		strings.Repeat("a", 63),
		strings.Repeat("a", 64),
		"invalid.namespace",
	} {
		t.Run(namespace, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: "edge", Namespace: new(namespace)}}}}}}}}}}
			before := tnt.DeepCopy()
			for _, response := range []*admission.Response{
				h.OnCreate(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}),
				h.OnUpdate(nil, nil, tnt, before, nil, nil)(t.Context(), admission.Request{}),
			} {
				if namespace == "invalid.namespace" || namespace == strings.Repeat("a", 64) {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "ingress.listenerSets.parentRef.namespace")
				} else {
					require.Nil(t, response)
				}
			}
			require.Equal(t, before, tnt)
		})
	}
}

func TestListenerSetRuleValidation(t *testing.T) {
	h := RuleHandler(nil, nil)
	for _, name := range []string{"edge", "{{ .tenant.metadata.name }}", "bad/name", ""} {
		t.Run(name, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: name}}}}}}}}}}
			before := tnt.DeepCopy()
			for _, response := range []*admission.Response{
				h.OnCreate(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}),
				h.OnUpdate(nil, nil, tnt, before, nil, nil)(t.Context(), admission.Request{}),
			} {
				if name == "bad/name" || name == "" {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "ingress.listenerSets.parentRef.name")
				} else {
					require.Nil(t, response)
				}
			}
			require.Equal(t, before, tnt)
		})
	}
}
