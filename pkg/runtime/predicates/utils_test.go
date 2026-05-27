// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestLabelsEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil and empty", nil, map[string]string{}, true},
		{"same single label", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"different value", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"different key", map[string]string{"a": "1"}, map[string]string{"b": "1"}, false},
		{"missing key in b", map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}, false},
		{"same multiple labels (order independent)", map[string]string{"b": "2", "a": "1"}, map[string]string{"a": "1", "b": "2"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := predicates.LabelsEqual(tt.a, tt.b); got != tt.want {
				t.Fatalf("labelsEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLabelsChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		keys      []string
		oldLabels map[string]string
		newLabels map[string]string
		want      bool
	}{
		{
			name:      "no keys returns false",
			keys:      nil,
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "2"},
			want:      false,
		},
		{
			name:      "key absent in both returns false",
			keys:      []string{"x"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "2"},
			want:      false,
		},
		{
			name:      "key added returns true",
			keys:      []string{"x"},
			oldLabels: map[string]string{"a": "1"},
			newLabels: map[string]string{"a": "1", "x": "v"},
			want:      true,
		},
		{
			name:      "key removed returns true",
			keys:      []string{"x"},
			oldLabels: map[string]string{"a": "1", "x": "v"},
			newLabels: map[string]string{"a": "1"},
			want:      true,
		},
		{
			name:      "key value changed returns true",
			keys:      []string{"x"},
			oldLabels: map[string]string{"x": "v1"},
			newLabels: map[string]string{"x": "v2"},
			want:      true,
		},
		{
			name:      "key unchanged returns false",
			keys:      []string{"x"},
			oldLabels: map[string]string{"x": "v"},
			newLabels: map[string]string{"x": "v"},
			want:      false,
		},
		{
			name:      "multiple keys returns true if any tracked key changed",
			keys:      []string{"a", "b", "c"},
			oldLabels: map[string]string{"a": "1", "b": "2", "c": "3"},
			newLabels: map[string]string{"a": "1", "b": "CHANGED", "c": "3"},
			want:      true,
		},
		{
			name:      "multiple keys returns false if none of tracked keys changed even if other labels changed",
			keys:      []string{"a", "b"},
			oldLabels: map[string]string{"a": "1", "b": "2", "other": "x"},
			newLabels: map[string]string{"a": "1", "b": "2", "other": "y"},
			want:      false,
		},
		{
			name:      "nil maps behave like empty maps (added triggers true)",
			keys:      []string{"x"},
			oldLabels: nil,
			newLabels: map[string]string{"x": "v"},
			want:      true,
		},
		{
			name:      "nil maps behave like empty maps (both nil no change -> false)",
			keys:      []string{"x"},
			oldLabels: nil,
			newLabels: nil,
			want:      false,
		},
		{
			name:      "empty string value vs missing key counts as change",
			keys:      []string{"x"},
			oldLabels: map[string]string{"x": ""},
			newLabels: map[string]string{},
			want:      true,
		},
		{
			name:      "duplicate keys still works (change detected once)",
			keys:      []string{"x", "x"},
			oldLabels: map[string]string{"x": "v1"},
			newLabels: map[string]string{"x": "v2"},
			want:      true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := predicates.LabelsChanged(tt.keys, tt.oldLabels, tt.newLabels)
			if got != tt.want {
				t.Fatalf("LabelsChanged(%v, %v, %v) = %v, want %v",
					tt.keys, tt.oldLabels, tt.newLabels, got, tt.want)
			}
		})
	}
}
