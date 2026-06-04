// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"net"
	"reflect"
	"testing"
)

func TestIPsToStrings(t *testing.T) {
	t.Parallel()

	ips := []net.IP{
		net.ParseIP("10.96.0.20"),
		nil,
		net.ParseIP("10.96.0.10"),
		net.ParseIP("fd00::1"),
	}

	expected := []string{
		"10.96.0.10",
		"10.96.0.20",
		"fd00::1",
	}

	if got := IPsToStrings(ips); !reflect.DeepEqual(expected, got) {
		t.Fatalf("expected IP strings %v, got %v", expected, got)
	}
}
