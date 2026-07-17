// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func TestTenantOwnerAggregateEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		aggregate *bool
		want      bool
	}{
		{name: "unset defaults true", want: true},
		{name: "explicit true", aggregate: ptr.To(true), want: true},
		{name: "explicit false", aggregate: ptr.To(false), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := TenantOwnerSpec{Aggregate: tt.aggregate}
			if got := spec.AggregateEnabled(); got != tt.want {
				t.Fatalf("AggregateEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTenantOwnerAggregateFalseIsSerialized(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(TenantOwnerSpec{Aggregate: ptr.To(false)})
	if err != nil {
		t.Fatalf("marshal TenantOwnerSpec: %v", err)
	}
	if !strings.Contains(string(data), `"aggregate":false`) {
		t.Fatalf("explicit false was omitted from JSON: %s", data)
	}
}
