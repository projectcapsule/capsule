// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeterministicUUID(t *testing.T) {
	tests := []struct {
		name      string
		partsA    []string
		partsB    []string
		sameAsA   bool
		wantUpper bool
	}{
		{
			name:      "same inputs produce same uuid",
			partsA:    []string{"tenant", "wind", "ns", "wind-test"},
			partsB:    []string{"tenant", "wind", "ns", "wind-test"},
			sameAsA:   true,
			wantUpper: true,
		},
		{
			name:      "whitespace is trimmed (same logical inputs)",
			partsA:    []string{" tenant ", " wind", "ns ", " wind-test "},
			partsB:    []string{"tenant", "wind", "ns", "wind-test"},
			sameAsA:   true,
			wantUpper: true,
		},
		{
			name:      "different inputs produce different uuid",
			partsA:    []string{"tenant", "wind", "ns", "wind-test"},
			partsB:    []string{"tenant", "wind", "ns", "other-ns"},
			sameAsA:   false,
			wantUpper: true,
		},
		{
			name:      "empty strings are kept as separators (affects output)",
			partsA:    []string{"a", "", "b"},
			partsB:    []string{"a", "b"},
			sameAsA:   false,
			wantUpper: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uA := deterministicUUID(tt.partsA...)
			uB := deterministicUUID(tt.partsB...)

			// Basic UUID formatting checks
			assert.Len(t, uA, 36)
			assert.Equal(t, byte('-'), uA[8])
			assert.Equal(t, byte('-'), uA[13])
			assert.Equal(t, byte('-'), uA[18])
			assert.Equal(t, byte('-'), uA[23])

			if tt.wantUpper {
				assert.Equal(t, strings.ToUpper(uA), uA)
			}

			// Version 5 and RFC4122 variant checks:
			// UUID format: xxxxxxxx-xxxx-Mxxx-Nxxx-xxxxxxxxxxxx
			// M must be '5' (version 5). N must be one of 8,9,A,B (variant 10xx)
			assert.Equal(t, byte('5'), uA[14], "expected version 5 at position 14")
			assert.Contains(t, "89AB", string(uA[19]), "expected RFC4122 variant at position 19")

			if tt.sameAsA {
				assert.Equal(t, uA, uB)
			} else {
				assert.NotEqual(t, uA, uB)
			}
		})
	}
}
