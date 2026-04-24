// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"strings"
	"testing"
)

func TestWrapJSONPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourcePath string
		want       string
	}{
		{
			name:       "simple path",
			sourcePath: ".spec.resources.requests.cpu",
			want:       "{.spec.resources.requests.cpu}",
		},
		{
			name:       "empty path",
			sourcePath: "",
			want:       "{}",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := wrapJSONPath(tt.sourcePath)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidateSourcePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourcePath string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid path",
			sourcePath: ".spec.resources.requests.cpu",
		},
		{
			name:       "valid minimal path",
			sourcePath: ".a",
		},
		{
			name:       "empty path",
			sourcePath: "",
			wantErr:    true,
			errMsg:     "sourcePath must not be empty",
		},
		{
			name:       "too long",
			sourcePath: "." + strings.Repeat("a", maxJSONPathLength),
			wantErr:    true,
			errMsg:     "sourcePath exceeds max length",
		},
		{
			name:       "missing dot prefix",
			sourcePath: "spec.value",
			wantErr:    true,
			errMsg:     "sourcePath must start with '.'",
		},
		{
			name:       "contains carriage return",
			sourcePath: ".spec\r.value",
			wantErr:    true,
			errMsg:     "sourcePath must not contain control whitespace",
		},
		{
			name:       "contains newline",
			sourcePath: ".spec\nvalue",
			wantErr:    true,
			errMsg:     "sourcePath must not contain control whitespace",
		},
		{
			name:       "contains tab",
			sourcePath: ".spec\tvalue",
			wantErr:    true,
			errMsg:     "sourcePath must not contain control whitespace",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSourcePath(tt.sourcePath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
