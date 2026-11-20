// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestReplicationHealthIsOptInAndDeepCopied(t *testing.T) {
	parent := &TenantResource{}
	require.NoError(t, json.Unmarshal([]byte(`{"spec":{"resyncPeriod":"60s","resources":[]}}`), parent))
	require.Nil(t, parent.Spec.HealthChecks)
	require.False(t, parent.Spec.HealthChecks.Enabled())
	require.Nil(t, parent.Status.Health)
	data, err := json.Marshal(parent)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"wait"`)
	require.NotContains(t, string(data), `"healthChecks"`)
	require.NotContains(t, string(data), `"health"`)
	parent.Spec.HealthChecks = &TenantResourceHealthChecks{Wait: true, Timeout: &metav1.Duration{Duration: time.Minute}, Expressions: []TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: "true"}}}
	data, err = json.Marshal(parent)
	require.NoError(t, err)
	var encoded struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(data, &encoded))
	require.JSONEq(t, `{"wait":true,"timeout":"1m0s","expressions":[{"apiVersion":"v1","kind":"ConfigMap","current":"true"}]}`, string(encoded.Spec["healthChecks"]))
	for _, flat := range []string{"wait", "healthCheckTimeout", "healthCheckExprs"} {
		require.NotContains(t, encoded.Spec, flat)
	}
	parent.Status.Health = &TenantResourceHealthStatus{Revision: "one", Cursor: 2}
	parent.Status.ProcessedItems = meta.ProcessedItems{{Health: &meta.ResourceHealthReference{UID: "target", Digest: "first"}}}
	clone := parent.DeepCopy()
	clone.Spec.HealthChecks.Wait = false
	clone.Spec.HealthChecks.Timeout.Duration = time.Hour
	clone.Spec.HealthChecks.Expressions[0].Current = "false"
	clone.Status.Health.Revision = "two"
	clone.Status.ProcessedItems[0].Health.Digest = "other"
	require.True(t, parent.Spec.HealthChecks.Enabled())
	require.Equal(t, time.Minute, parent.Spec.HealthChecks.Timeout.Duration)
	require.Equal(t, "true", parent.Spec.HealthChecks.Expressions[0].Current)
	require.Equal(t, "one", parent.Status.Health.Revision)
	require.Equal(t, "first", parent.Status.ProcessedItems[0].Health.Digest)
	global := &GlobalTenantResource{Spec: GlobalTenantResourceSpec{TenantResourceCommonSpec: parent.Spec.TenantResourceCommonSpec}}
	globalCopy := global.DeepCopy()
	globalCopy.Spec.HealthChecks.Timeout.Duration = time.Hour
	globalCopy.Spec.HealthChecks.Expressions[0].Current = "false"
	require.Equal(t, time.Minute, global.Spec.HealthChecks.Timeout.Duration)
	require.Equal(t, "true", global.Spec.HealthChecks.Expressions[0].Current)
}

func TestNestedReplicationHealthChecksJSON(t *testing.T) {
	for _, tc := range []struct {
		name, input     string
		absent, enabled bool
		timeout         time.Duration
	}{
		{name: "legacy", input: `{}`, absent: true},
		{name: "null", input: `{"healthChecks":null}`, absent: true},
		{name: "empty", input: `{"healthChecks":{}}`},
		{name: "disabled", input: `{"healthChecks":{"wait":false,"timeout":"10s","expressions":[{"apiVersion":"v1","kind":"ConfigMap","current":"true"}]}}`, timeout: 10 * time.Second},
		{name: "enabled", input: `{"healthChecks":{"wait":true,"timeout":"10s"}}`, enabled: true, timeout: 10 * time.Second},
		{name: "default timeout", input: `{"healthChecks":{"wait":true}}`, enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spec TenantResourceCommonSpec
			require.NoError(t, json.Unmarshal([]byte(tc.input), &spec))
			require.Equal(t, tc.absent, spec.HealthChecks == nil)
			require.Equal(t, tc.enabled, spec.HealthChecks.Enabled())
			if tc.timeout != 0 {
				require.NotNil(t, spec.HealthChecks.Timeout)
				require.Equal(t, tc.timeout, spec.HealthChecks.Timeout.Duration)
			} else if spec.HealthChecks != nil {
				require.Nil(t, spec.HealthChecks.Timeout, "the controller supplies the existing timeout default")
			}
			data, err := json.Marshal(spec)
			require.NoError(t, err)
			var roundTrip TenantResourceCommonSpec
			require.NoError(t, json.Unmarshal(data, &roundTrip))
			require.Equal(t, spec, roundTrip)
		})
	}
}
