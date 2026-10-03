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
		`{"placement":{"scheduler":"team"},"security":{"readOnlyRootFilesystem":true},"registries":{"imagePullPolicy":"Always","imagePullSecrets":[]}}`,
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

func TestWorkloadMutationImagePullPolicyPresence(t *testing.T) {
	for _, value := range []corev1.PullPolicy{"", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever} {
		original := WorkloadMutation{Registries: WorkloadRegistryMutation{ImagePullPolicy: value}}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		var decoded WorkloadMutation
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, original, decoded)
		require.Equal(t, original, *original.DeepCopy())
		if value == "" {
			require.NotContains(t, string(data), "registries")
		} else {
			require.JSONEq(t, `{"registries":{"imagePullPolicy":"`+string(value)+`"}}`, string(data))
		}
	}
	var workload WorkloadMutation
	require.Error(t, json.Unmarshal([]byte(`{"registries":{"imagePullPolicy":false}}`), &workload))
	require.NoError(t, json.Unmarshal([]byte(`{"registries":{"imagePullPolicy":null}}`), &workload))
	require.Empty(t, workload.Registries.ImagePullPolicy)
}

func TestWorkloadMutationRegistryOmission(t *testing.T) {
	for _, data := range []string{`{}`, `{"registries":null}`, `{"registries":{}}`, `{"registries":{"imagePullPolicy":null}}`} {
		var workload WorkloadMutation
		require.NoError(t, json.Unmarshal([]byte(data), &workload))
		require.Empty(t, workload.Registries.ImagePullPolicy)
		encoded, err := json.Marshal(workload)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(encoded))
	}
}

func TestWorkloadMutationImagePullSecretsPresenceAndDeepCopy(t *testing.T) {
	for _, secrets := range [][]corev1.LocalObjectReference{nil, {}, {{Name: "tenant-registry"}}} {
		original := WorkloadMutation{Registries: WorkloadRegistryMutation{ImagePullSecrets: secrets}}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		var decoded WorkloadMutation
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, original, decoded)
		copied := original.DeepCopy()
		require.Equal(t, original, *copied)
		if secrets == nil {
			require.JSONEq(t, `{}`, string(data))
		} else if len(secrets) == 0 {
			require.JSONEq(t, `{"registries":{"imagePullSecrets":[]}}`, string(data))
		} else {
			require.JSONEq(t, `{"registries":{"imagePullSecrets":[{"name":"tenant-registry"}]}}`, string(data))
			copied.Registries.ImagePullSecrets[0].Name = "changed"
			require.Equal(t, "tenant-registry", original.Registries.ImagePullSecrets[0].Name)
		}
	}
	var workload WorkloadMutation
	require.NoError(t, json.Unmarshal([]byte(`{"registries":{"imagePullSecrets":null}}`), &workload))
	require.Nil(t, workload.Registries.ImagePullSecrets)
	require.Error(t, json.Unmarshal([]byte(`{"registries":{"imagePullSecrets":["secret-name"]}}`), &workload))
}
