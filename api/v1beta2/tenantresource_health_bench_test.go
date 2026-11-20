// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
)

func BenchmarkReplicationHealthSpecDeepCopy(b *testing.B) {
	expressions := make([]map[string]any, 32)
	for i := range expressions {
		expressions[i] = map[string]any{"apiVersion": "example.projectcapsule.dev/v1", "kind": fmt.Sprintf("Example%d", i), "current": "has(object.status) && object.status.ready == true"}
	}
	for _, scenario := range []struct {
		name   string
		config map[string]any
	}{
		{name: "omitted"},
		{name: "disabled", config: map[string]any{"wait": false, "timeout": "10s"}},
		{name: "enabled", config: map[string]any{"wait": true, "timeout": "10s"}},
		{name: "expressions=32", config: map[string]any{"wait": true, "timeout": "10s", "expressions": expressions}},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			payload := map[string]any{}
			if scenario.config != nil {
				payload["healthChecks"] = scenario.config
			}
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			var spec TenantResourceCommonSpec
			if err := json.Unmarshal(data, &spec); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				runtime.KeepAlive(spec.DeepCopy())
			}
		})
	}
}
