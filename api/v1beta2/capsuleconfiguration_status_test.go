// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
)

func TestCapsuleConfigurationStatusSerializesEmptyTenants(t *testing.T) {
	t.Parallel()

	status := CapsuleConfigurationStatus{Tenants: make([]string, 0)}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if got, want := string(data), `{"tenants":[]}`; got != want {
		t.Fatalf("marshaled status = %s, want %s", got, want)
	}

	var decoded CapsuleConfigurationStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if decoded.Tenants == nil {
		t.Fatal("tenants must remain initialized after a JSON round trip")
	}
}
