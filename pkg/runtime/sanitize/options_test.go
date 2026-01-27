// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
)

func TestDefaultSanitizeOptions(t *testing.T) {
	opts := sanitize.DefaultSanitizeOptions()

	if !opts.StripManagedFields {
		t.Fatalf("expected StripManagedFields=true")
	}
	if !opts.StripLastApplied {
		t.Fatalf("expected StripLastApplied=true")
	}
	if !opts.StripStatus {
		t.Fatalf("expected StripStatus=true")
	}
	if !opts.StripUID {
		t.Fatalf("expected StripUID=true")
	}
}
