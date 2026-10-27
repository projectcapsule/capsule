// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestOptionalMetadataBooleanRepresentations(t *testing.T) {
	variants := []struct {
		name, value string
		enabled     bool
	}{
		{"omitted", "", false},
		{"null", ":null", false},
		{"explicit false", ":false", false},
		{"explicit true", ":true", true},
	}
	for _, negate := range variants {
		for _, required := range variants {
			t.Run(negate.name+"/"+required.name, func(t *testing.T) {
				var negateField, requiredField string
				if negate.value != "" {
					negateField = `,"negate"` + negate.value
				}
				if required.value != "" {
					requiredField = `,"required"` + required.value
				}
				source := fmt.Sprintf(`{"enforce":{"action":"allow","metadata":[{"apiGroups":["v1"],"kinds":["ConfigMap"],"labels":{"team":{"values":[{"exp":"^alpha$"%s}]%s}}}]}}`, negateField, requiredField)
				var body rules.NamespaceRuleBodyNamespace
				require.NoError(t, json.Unmarshal([]byte(source), &body))
				before := body.DeepCopy()
				roundTrip, err := json.Marshal(&body)
				require.NoError(t, err)
				if !negate.enabled {
					require.NotContains(t, string(roundTrip), `"negate"`)
				}
				if !required.enabled {
					require.NotContains(t, string(roundTrip), `"required"`)
				}
				h := GenericRules(nil)
				for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
					for _, value := range []string{"", "alpha", "beta"} {
						t.Run(string(operation)+"/"+value, func(t *testing.T) {
							labels := map[string]string{}
							if value != "" {
								labels["team"] = value
							}
							obj := genericMetadataObject(labels, nil)
							req := admissionRequest("v1", "ConfigMap")
							req.Operation = operation
							var response *admission.Response
							if operation == admissionv1.Create {
								response = h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, testTenant(), []*rules.NamespaceRuleBodyNamespace{&body})(t.Context(), req)
							} else {
								response = h.OnUpdate(nil, nil, genericMetadataObject(nil, nil), obj, nil, testEventRecorder{}, testTenant(), []*rules.NamespaceRuleBodyNamespace{&body})(t.Context(), req)
							}
							denied := required.enabled
							if value != "" {
								denied = (value == "alpha") == negate.enabled
							}
							if denied {
								require.NotNil(t, response)
								require.False(t, response.Allowed)
								require.Contains(t, response.Result.Message, "metadata label")
							} else {
								require.Nil(t, response, "allowed metadata must continue the handler chain")
							}
						})
					}
				}
				require.Equal(t, before, &body)
			})
		}
	}
}
