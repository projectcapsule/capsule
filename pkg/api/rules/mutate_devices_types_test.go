// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceMutationRoundTripAndCopy(t *testing.T) {
	for _, raw := range []string{`{}`, `{"devices":null}`, `{"devices":{"class":null}}`, `{"devices":{"class":"gpu-default"}}`} {
		var mutation NamespaceRuleMutation
		require.NoError(t, json.Unmarshal([]byte(raw), &mutation))
		copy := mutation.DeepCopy()
		require.Equal(t, mutation, *copy)
		encoded, err := json.Marshal(copy)
		require.NoError(t, err)
		var decoded NamespaceRuleMutation
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.Equal(t, mutation, decoded)
		if mutation.Devices.Class == "" {
			require.NotContains(t, string(encoded), "devices")
		}
		copy.Devices.Class = "different"
		require.NotEqual(t, mutation.Devices.Class, copy.Devices.Class)
	}
}
