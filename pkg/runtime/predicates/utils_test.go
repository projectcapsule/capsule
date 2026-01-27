// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestLabelsEqual(t *testing.T) {
	t.Parallel()

	type tc struct {
		name string
		a    map[string]string
		b    map[string]string
		want bool
	}

	tests := []tc{
		{
			name: "both nil => equal",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "nil vs empty => equal (len==0)",
			a:    nil,
			b:    map[string]string{},
			want: true,
		},
		{
			name: "empty vs nil => equal (len==0)",
			a:    map[string]string{},
			b:    nil,
			want: true,
		},
		{
			name: "same single entry => equal",
			a:    map[string]string{"a": "1"},
			b:    map[string]string{"a": "1"},
			want: true,
		},
		{
			name: "same entries different insertion order => equal",
			a:    map[string]string{"a": "1", "b": "2"},
			b:    map[string]string{"b": "2", "a": "1"},
			want: true,
		},
		{
			name: "different lengths => not equal",
			a:    map[string]string{"a": "1"},
			b:    map[string]string{"a": "1", "b": "2"},
			want: false,
		},
		{
			name: "missing key in b => not equal",
			a:    map[string]string{"a": "1", "b": "2"},
			b:    map[string]string{"a": "1", "c": "2"},
			want: false,
		},
		{
			name: "same keys but different value => not equal",
			a:    map[string]string{"a": "1"},
			b:    map[string]string{"a": "2"},
			want: false,
		},
		{
			name: "b has extra key (len differs) => not equal",
			a:    map[string]string{"a": "1"},
			b:    map[string]string{"a": "1", "x": "y"},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := predicates.LabelsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("LabelsEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLabelsChanged(t *testing.T) {
	t.Parallel()

	type tc struct {
		name      string
		keys      []string
		oldLabels map[string]string
		newLabels map[string]string
		want      bool
	}

	tests := []tc{
		{
			name:      "no keys => unchanged (false)",
			keys:      nil,
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "2"},
			want:      false,
		},
		{
			name:      "key unchanged => false",
			keys:      []string{"a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "1"},
			want:      false,
		},
		{
			name:      "value changed => true",
			keys:      []string{"a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "2"},
			want:      true,
		},
		{
			name:      "key added => true",
			keys:      []string{"a"},
			oldLabels: map[string]string{},
			newLabels: map[string]string{"a": "1"},
			want:      true,
		},
		{
			name:      "key removed => true",
			keys:      []string{"a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{},
			want:      true,
		},
		{
			name:      "old nil new has key => true",
			keys:      []string{"a"},
			oldLabels: nil,
			newLabels: map[string]string{"a": "1"},
			want:      true,
		},
		{
			name:      "old has key new nil => true",
			keys:      []string{"a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: nil,
			want:      true,
		},
		{
			name:      "both nil and key missing => false",
			keys:      []string{"a"},
			oldLabels: nil,
			newLabels: nil,
			want:      false,
		},
		{
			name:      "multiple keys: one changed => true",
			keys:      []string{"a", "b"},
			oldLabels: map[string]string{"a": "1", "b": "2"},
			newLabels: map[string]string{"a": "1", "b": "3"},
			want:      true,
		},
		{
			name:      "multiple keys: only non-watched key changed => false",
			keys:      []string{"a"},
			oldLabels: map[string]string{"a": "1", "x": "old"},
			newLabels: map[string]string{"a": "1", "x": "new"},
			want:      false,
		},
		{
			name:      "watched key absent in both even if other keys differ => false",
			keys:      []string{"a"},
			oldLabels: map[string]string{"x": "1"},
			newLabels: map[string]string{"x": "2"},
			want:      false,
		},
		{
			name:      "duplicate keys in keys slice still behaves correctly",
			keys:      []string{"a", "a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "1"},
			want:      false,
		},
		{
			name:      "duplicate keys in keys slice with change => true",
			keys:      []string{"a", "a"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "2"},
			want:      true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := predicates.LabelsChanged(tt.keys, tt.oldLabels, tt.newLabels)
			if got != tt.want {
				t.Fatalf("LabelsChanged(keys=%v, old=%v, new=%v) = %v, want %v",
					tt.keys, tt.oldLabels, tt.newLabels, got, tt.want,
				)
			}
		})
	}
}
