// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkloadMutationRootFilesystemPresence(t *testing.T) {
	for _, value := range []*bool{nil, new(false), new(true)} {
		original := WorkloadMutation{ReadOnlyRootFilesystem: value, Targets: []WorkloadValidationTarget{ValidateInitContainers}}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		var decoded WorkloadMutation
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, original, decoded)
		if value == nil {
			require.NotContains(t, string(data), "readOnlyRootFilesystem")
		} else {
			require.Contains(t, string(data), "readOnlyRootFilesystem")
		}
		copied := original.DeepCopy()
		copied.Targets[0] = ValidatePod
		require.Equal(t, ValidateInitContainers, original.Targets[0])
		if value != nil {
			*copied.ReadOnlyRootFilesystem = !*value
			require.NotEqual(t, *copied.ReadOnlyRootFilesystem, *original.ReadOnlyRootFilesystem)
		}
	}
	var workload WorkloadMutation
	require.Error(t, json.Unmarshal([]byte(`{"readOnlyRootFilesystem":"false"}`), &workload))
	require.NoError(t, json.Unmarshal([]byte(`{"readOnlyRootFilesystem":null}`), &workload))
	require.Nil(t, workload.ReadOnlyRootFilesystem)
}
