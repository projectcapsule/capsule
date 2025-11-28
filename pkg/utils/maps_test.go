// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestMapMergeNoOverrite_AddsNonOverlapping(t *testing.T) {
	dst := map[string]string{"a": "1"}
	src := map[string]string{"b": "2"}

	utils.MapMergeNoOverrite(dst, src)

	if got, want := dst["a"], "1"; got != want {
		t.Fatalf("dst[a] = %q, want %q", got, want)
	}
	if got, want := dst["b"], "2"; got != want {
		t.Fatalf("dst[b] = %q, want %q", got, want)
	}
	if len(dst) != 2 {
		t.Fatalf("len(dst) = %d, want 2", len(dst))
	}
}

func TestMapMergeNoOverrite_DoesNotOverwriteExisting(t *testing.T) {
	dst := map[string]string{"a": "1"}
	src := map[string]string{"a": "X"} // overlapping key

	utils.MapMergeNoOverrite(dst, src)

	if got, want := dst["a"], "1"; got != want {
		t.Fatalf("dst[a] overwritten: got %q, want %q", got, want)
	}
}

func TestMapMergeNoOverrite_EmptySrc_NoChange(t *testing.T) {
	dst := map[string]string{"a": "1"}
	src := map[string]string{} // empty

	before := make(map[string]string, len(dst))
	for k, v := range dst {
		before[k] = v
	}

	utils.MapMergeNoOverrite(dst, src)

	if !reflect.DeepEqual(dst, before) {
		t.Fatalf("dst changed with empty src: got %#v, want %#v", dst, before)
	}
}

func TestMapMergeNoOverrite_NilSrc_NoChange(t *testing.T) {
	dst := map[string]string{"a": "1"}
	var src map[string]string // nil

	before := make(map[string]string, len(dst))
	for k, v := range dst {
		before[k] = v
	}

	utils.MapMergeNoOverrite(dst, src)

	if !reflect.DeepEqual(dst, before) {
		t.Fatalf("dst changed with nil src: got %#v, want %#v", dst, before)
	}
}

func TestMapMergeNoOverrite_Idempotent(t *testing.T) {
	dst := map[string]string{"a": "1"}
	src := map[string]string{"b": "2"}

	utils.MapMergeNoOverrite(dst, src)
	first := map[string]string{}
	for k, v := range dst {
		first[k] = v
	}

	// Call again; result should be identical
	utils.MapMergeNoOverrite(dst, src)

	if !reflect.DeepEqual(dst, first) {
		t.Fatalf("non-idempotent merge: after second merge got %#v, want %#v", dst, first)
	}
}

func TestMapMergeNoOverrite_NilDst_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when dst is nil, but did not panic")
		}
	}()

	var dst map[string]string // nil destination map
	src := map[string]string{"a": "1"}

	// Writing to a nil map panics; document current behavior via this test.
	utils.MapMergeNoOverrite(dst, src)
}
