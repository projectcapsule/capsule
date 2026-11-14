// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplicationTriggerDefaultsPreserveOldRepresentations(t *testing.T) {
	for _, obj := range []any{&TenantResource{}, &GlobalTenantResource{}} {
		encoded, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "triggerMinInterval") {
			t.Fatal("zero-value typed objects must omit interval so API defaulting applies")
		}
	}
}
