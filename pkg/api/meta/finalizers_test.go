// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestFilterFinalizers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		finalizers  []string
		ignored     map[string]struct{}
		want        []string
		wantRemoved bool
	}{
		{
			name:        "nil finalizers",
			finalizers:  nil,
			ignored:     map[string]struct{}{"keep.me": {}},
			want:        nil,
			wantRemoved: false,
		},
		{
			name:        "empty finalizers",
			finalizers:  []string{},
			ignored:     map[string]struct{}{"keep.me": {}},
			want:        nil,
			wantRemoved: false,
		},
		{
			name:        "empty ignored removes all",
			finalizers:  []string{"a", "b"},
			ignored:     map[string]struct{}{},
			want:        nil,
			wantRemoved: true,
		},
		{
			name:        "all finalizers ignored",
			finalizers:  []string{"a", "b"},
			ignored:     map[string]struct{}{"a": {}, "b": {}},
			want:        []string{"a", "b"},
			wantRemoved: false,
		},
		{
			name:        "some ignored some removed",
			finalizers:  []string{"a", "b", "c"},
			ignored:     map[string]struct{}{"b": {}},
			want:        []string{"b"},
			wantRemoved: true,
		},
		{
			name:        "none ignored all removed",
			finalizers:  []string{"a", "b", "c"},
			ignored:     map[string]struct{}{"x": {}},
			want:        nil,
			wantRemoved: true,
		},
		{
			name:        "duplicates preserved when ignored",
			finalizers:  []string{"a", "b", "a"},
			ignored:     map[string]struct{}{"a": {}},
			want:        []string{"a", "a"},
			wantRemoved: true,
		},
		{
			name:        "order preserved for ignored finalizers",
			finalizers:  []string{"c", "a", "b"},
			ignored:     map[string]struct{}{"b": {}, "c": {}},
			want:        []string{"c", "b"},
			wantRemoved: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotRemoved := meta.FilterFinalizers(tt.finalizers, tt.ignored)

			if gotRemoved != tt.wantRemoved {
				t.Fatalf("FilterFinalizers() removed = %v, want %v", gotRemoved, tt.wantRemoved)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FilterFinalizers() finalizers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildFinalizersMergePatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		finalizers []string
		wantJSON   string
	}{
		{
			name:       "nil finalizers",
			finalizers: nil,
			wantJSON:   `{"metadata":{"finalizers":[]}}`,
		},
		{
			name:       "empty finalizers",
			finalizers: []string{},
			wantJSON:   `{"metadata":{"finalizers":[]}}`,
		},
		{
			name:       "single finalizer",
			finalizers: []string{"example.com/test"},
			wantJSON:   `{"metadata":{"finalizers":["example.com/test"]}}`,
		},
		{
			name:       "multiple finalizers",
			finalizers: []string{"a", "b", "c"},
			wantJSON:   `{"metadata":{"finalizers":["a","b","c"]}}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := meta.BuildFinalizersMergePatch(tt.finalizers)

			if string(got) != tt.wantJSON {
				t.Fatalf("BuildFinalizersMergePatch() = %s, want %s", string(got), tt.wantJSON)
			}
		})
	}
}

func TestBuildFinalizersMergePatch_ProducesValidJSON(t *testing.T) {
	t.Parallel()

	got := meta.BuildFinalizersMergePatch([]string{"a", "b"})

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("BuildFinalizersMergePatch() produced invalid JSON: %v", err)
	}

	metadata, ok := decoded["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata field missing or wrong type: %#v", decoded["metadata"])
	}

	finalizers, ok := metadata["finalizers"].([]any)
	if !ok {
		t.Fatalf("finalizers field missing or wrong type: %#v", metadata["finalizers"])
	}

	if len(finalizers) != 2 || finalizers[0] != "a" || finalizers[1] != "b" {
		t.Fatalf("unexpected finalizers: %#v", finalizers)
	}
}
