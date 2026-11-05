// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIngressClassesSerializationAndIsolation(t *testing.T) {
	const source = `{"types":["Ingress","Gateway"],"classes":[{"exact":["edge"],"exp":"^public-","matchLabels":{"env":"customer"}}]}`
	var original NamespaceRuleEnforceIngressBody
	require.NoError(t, json.Unmarshal([]byte(source), &original))
	copy := original.DeepCopy()
	encoded, err := json.Marshal(copy)
	require.NoError(t, err)
	require.JSONEq(t, source, string(encoded))
	copy.Classes[0].Exact[0] = "other"
	copy.Classes[0].MatchLabels["env"] = "other"
	copy.Types[0] = IngressTypeRoute
	require.Equal(t, "edge", original.Classes[0].Exact[0])
	require.Equal(t, "customer", original.Classes[0].MatchLabels["env"])
	require.Equal(t, IngressTypeIngress, original.Types[0])
	mutation := IngressMutation{Types: []IngressType{IngressTypeIngress}, Class: "edge"}
	mutation.DeepCopy().Types[0] = IngressTypeGateway
	require.Equal(t, IngressTypeIngress, mutation.Types[0])
}
