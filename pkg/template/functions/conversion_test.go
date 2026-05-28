// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToTOML(t *testing.T) {
	tests := []struct {
		name        string
		in          any
		expectError bool
	}{
		{
			name: "encodes simple map",
			in: map[string]any{
				"a": "b",
				"n": int64(3),
			},
			expectError: false,
		},
		{
			name:        "encodes nil as empty string or valid toml",
			in:          nil,
			expectError: false,
		},
		{
			name:        "returns error string on unsupported type (function)",
			in:          func() {},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toTOML(tt.in)
			require.NotNil(t, got)

			if tt.expectError {
				// Encoder errors are returned as strings, so just assert it's non-empty and looks like an error.
				assert.NotEmpty(t, got)
				return
			}

			// For successful encodes, output should be non-empty for most values.
			// (nil may encode to empty)
			if tt.in != nil {
				assert.NotEmpty(t, got)
			}
		})
	}
}

func TestFromTOML(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		expectError bool
		wantKeys    map[string]any
	}{
		{
			name: "valid toml",
			in:   "a = \"b\"\nn = 3\n",
			wantKeys: map[string]any{
				"a": "b",
				// go-toml commonly decodes numbers as int64
				"n": int64(3),
			},
		},
		{
			name:        "invalid toml sets Error key",
			in:          "a = ",
			expectError: true,
		},
		{
			name:     "empty string yields empty map (no Error)",
			in:       "",
			wantKeys: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromTOML(tt.in)

			if tt.expectError {
				errVal, ok := got["Error"]
				require.True(t, ok, "expected Error key")
				s, ok := errVal.(string)
				require.True(t, ok, "expected Error value to be string")
				require.NotEmpty(t, s)
				return
			}

			// Must NOT contain Error
			_, ok := got["Error"]
			require.False(t, ok, "did not expect Error key")

			assert.Equal(t, tt.wantKeys, got)
		})
	}
}

func TestFromJSONArray(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		expectError bool
		want        []any
	}{
		{
			name: "valid json array",
			in:   `["a","b",3,true]`,
			want: []any{"a", "b", float64(3), true}, // encoding/json uses float64 for numbers in interface{}
		},
		{
			name:        "invalid json returns single error string element",
			in:          `[`,
			expectError: true,
		},
		{
			name: "empty string returns error string element (invalid json)",
			in:   "",
			// json.Unmarshal("", &a) => error ("unexpected end of JSON input")
			expectError: true,
		},
		{
			name: "whitespace is invalid json (still error)",
			in:   "   ",
			// json.Unmarshal("   ", &a) => error
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromJSONArray(tt.in)
			if tt.expectError {
				require.Len(t, got, 1)
				_, ok := got[0].(string)
				require.True(t, ok, "expected error string in array")
				require.NotEmpty(t, got[0].(string))
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
