// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import "testing"

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "simple_without_slash",
			in:   "validate",
			want: "/validate",
		},
		{
			name: "already_has_slash",
			in:   "/validate",
			want: "/validate",
		},
		{
			name: "multiple_leading_slashes",
			in:   "///validate",
			want: "/validate",
		},
		{
			name: "trailing_slash",
			in:   "/validate/",
			want: "/validate",
		},
		{
			name: "double_slashes_inside",
			in:   "/foo//bar",
			want: "/foo/bar",
		},
		{
			name: "relative_dot_segment",
			in:   "/foo/./bar",
			want: "/foo/bar",
		},
		{
			name: "parent_segment",
			in:   "/foo/bar/../baz",
			want: "/foo/baz",
		},
		{
			name: "complex_mix",
			in:   "///foo//bar/../baz/",
			want: "/foo/baz",
		},
		{
			name: "only_slashes",
			in:   "////",
			want: "/",
		},
		{
			name: "root",
			in:   "/",
			want: "/",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalizePath(tt.in)
			if got != tt.want {
				t.Fatalf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
