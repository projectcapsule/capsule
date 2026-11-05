// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestGatewayClassMutationVersions(t *testing.T) {
	for _, version := range []string{"v1", "v1beta1"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, tc := range []struct {
				name, current, subresource string
				action                     rules.MutationAction
				changed                    bool
			}{
				{name: "merge default", changed: true},
				{name: "merge preserve", current: "explicit"},
				{name: "replace", current: "explicit", action: rules.MutationActionReplace, changed: true},
				{name: "replace identical", current: "edge", action: rules.MutationActionReplace},
				{name: "status skip", current: "explicit", action: rules.MutationActionReplace, subresource: "status"},
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", version, operation, tc.name), func(t *testing.T) {
					obj := ingressClassObject(rules.IngressTypeGateway)
					obj.SetAPIVersion("gateway.networking.k8s.io/" + version)
					if tc.current != "" {
						require.NoError(t, unstructured.SetNestedField(obj.Object, tc.current, "spec", "gatewayClassName"))
					}
					old := obj.DeepCopy()
					before := old.DeepCopy()
					req := listenerSetRequest(t, obj, operation, tc.subresource)
					body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{ingressClassMutation(rules.IngressTypeGateway, tc.action, "edge")}}
					handler := MetadataRules(nil)
					handle := handler.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})
					if operation == admissionv1.Update {
						handle = handler.OnUpdate(nil, nil, old, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})
					}
					response := handle(t.Context(), req)
					want := tc.current
					if tc.changed {
						require.NotNil(t, response, "served compatibility versions must receive mutations")
						require.True(t, response.Allowed)
						require.NotEmpty(t, response.Patches)
						want = "edge"
					} else {
						require.Nil(t, response)
					}
					actual, _, err := unstructured.NestedString(obj.Object, "spec", "gatewayClassName")
					require.NoError(t, err)
					require.Equal(t, want, actual)
					require.Equal(t, "gateway.networking.k8s.io/"+version, obj.GetAPIVersion())
					require.Equal(t, before, old)
				})
			}
		}
	}
}
