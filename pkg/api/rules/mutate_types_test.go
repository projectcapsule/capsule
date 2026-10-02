// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

func TestWorkloadMutationRootFilesystemPresence(t *testing.T) {
	for _, value := range []*bool{nil, new(false), new(true)} {
		original := WorkloadMutation{Security: WorkloadSecurityMutation{ReadOnlyRootFilesystem: value}, Targets: []WorkloadValidationTarget{ValidateInitContainers}}
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
			*copied.Security.ReadOnlyRootFilesystem = !*value
			require.NotEqual(t, *copied.Security.ReadOnlyRootFilesystem, *original.Security.ReadOnlyRootFilesystem)
		}
	}
	var workload WorkloadMutation
	require.Error(t, json.Unmarshal([]byte(`{"security": {"readOnlyRootFilesystem": "false"}}`), &workload))
	require.NoError(t, json.Unmarshal([]byte(`{"security": {"readOnlyRootFilesystem": null}}`), &workload))
	require.Nil(t, workload.Security.ReadOnlyRootFilesystem)
}

func TestNestedWorkloadMutationSerialization(t *testing.T) {
	for _, source := range []string{
		`{}`,
		`{"placement":{"nodeSelector":{},"tolerations":[],"topologySpreadConstraints":[],"affinity":{}}}`,
		`{"placement":{"scheduler":"team"},"security":{"hostUsers":false,"readOnlyRootFilesystem":false,"seccompProfile":{"type":"RuntimeDefault"},"appArmorProfile":{"type":"RuntimeDefault"}}}`,
	} {
		t.Run(source, func(t *testing.T) {
			var workload WorkloadMutation
			require.NoError(t, yaml.UnmarshalStrict([]byte(source), &workload))
			encoded, err := json.Marshal(workload.DeepCopy())
			require.NoError(t, err)
			require.JSONEq(t, source, string(encoded))
		})
	}
	for _, source := range []string{`{"placement":null,"security":null}`, `{"placement":{},"security":{}}`} {
		var workload WorkloadMutation
		require.NoError(t, yaml.UnmarshalStrict([]byte(source), &workload))
		encoded, err := json.Marshal(workload)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(encoded))
	}
	for _, field := range []string{"scheduler", "hostUsers", "readOnlyRootFilesystem", "seccompProfile", "appArmorProfile", "nodeSelector", "tolerations", "topologySpreadConstraints", "affinity"} {
		var workload WorkloadMutation
		require.ErrorContains(t, yaml.UnmarshalStrict([]byte(`{"`+field+`":null}`), &workload), "unknown field")
	}
	original := WorkloadMutation{
		Placement: WorkloadPlacementMutation{NodeSelector: map[string]string{"pool": "shared"}},
		Security:  WorkloadSecurityMutation{HostUsers: new(false), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
	}
	copy := original.DeepCopy()
	copy.Placement.NodeSelector["pool"] = "other"
	*copy.Security.HostUsers = true
	copy.Security.SeccompProfile.Type = corev1.SeccompProfileTypeUnconfined
	require.Equal(t, "shared", original.Placement.NodeSelector["pool"])
	require.False(t, *original.Security.HostUsers)
	require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, original.Security.SeccompProfile.Type)
}
