// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceClassRuleRoundTripAndIsolation(t *testing.T) {
	var body NamespaceRuleBodyNamespace
	require.NoError(t, json.Unmarshal([]byte(`{"enforce":{"action":"allow","devices":{"classes":[{"exact":["gpu.example.com"],"exp":"^gpu-","selector":{"matchExpressions":[{"key":"tenant","operator":"In","values":["a"]}]}},{"selector":{}}]}}}`), &body))
	require.Len(t, body.Enforce.Devices.Classes, 2)
	require.NotNil(t, body.Enforce.Devices.Classes[1].Selector)
	copy := body.DeepCopy()
	copy.Enforce.Devices.Classes[0].Exact[0] = "changed"
	copy.Enforce.Devices.Classes[0].Selector.MatchExpressions[0].Values[0] = "b"
	require.Equal(t, "gpu.example.com", body.Enforce.Devices.Classes[0].Exact[0])
	require.Equal(t, "a", body.Enforce.Devices.Classes[0].Selector.MatchExpressions[0].Values[0])
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"selector":{}`)
	var legacy NamespaceRuleBodyNamespace
	require.NoError(t, json.Unmarshal([]byte(`{"enforce":{"action":"deny"}}`), &legacy))
	require.Nil(t, legacy.Enforce.Devices.Classes)
}
