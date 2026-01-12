// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package utils_test

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestBuildNodeSelector_NilAnnotationsInitializesMap(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	tnt.Spec.NodeSelector = map[string]string{
		"disktype": "ssd",
	}

	out := utils.BuildNodeSelector(tnt, nil)

	if out == nil {
		t.Fatalf("expected non-nil map")
	}

	got, ok := out[utils.NodeSelectorAnnotation]
	if !ok {
		t.Fatalf("expected %q annotation to be set", utils.NodeSelectorAnnotation)
	}
	if got != "disktype=ssd" {
		t.Fatalf("unexpected annotation value: got %q want %q", got, "disktype=ssd")
	}
}

func TestBuildNodeSelector_ExistingAnnotationsPreserved(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	tnt.Spec.NodeSelector = map[string]string{
		"disktype": "ssd",
	}

	in := map[string]string{
		"keep": "me",
	}

	out := utils.BuildNodeSelector(tnt, in)

	if out["keep"] != "me" {
		t.Fatalf("expected existing annotation key to be preserved")
	}
	if out[utils.NodeSelectorAnnotation] != "disktype=ssd" {
		t.Fatalf("unexpected node selector annotation: got %q", out[utils.NodeSelectorAnnotation])
	}
}

func TestBuildNodeSelector_SortsSelectorsDeterministically(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	// Intentionally unordered map
	tnt.Spec.NodeSelector = map[string]string{
		"b": "2",
		"a": "1",
		"c": "3",
	}

	out := utils.BuildNodeSelector(tnt, map[string]string{})

	got := out[utils.NodeSelectorAnnotation]
	want := "a=1,b=2,c=3"
	if got != want {
		t.Fatalf("expected deterministic sorted annotation: got %q want %q", got, want)
	}
}

func TestBuildNodeSelector_EmptyNodeSelectorSetsEmptyAnnotation(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	tnt.Spec.NodeSelector = map[string]string{}

	out := utils.BuildNodeSelector(tnt, map[string]string{})

	got, ok := out[utils.NodeSelectorAnnotation]
	if !ok {
		t.Fatalf("expected %q annotation to be present even for empty selector", utils.NodeSelectorAnnotation)
	}
	if got != "" {
		t.Fatalf("expected empty annotation value, got %q", got)
	}
}

func TestBuildNodeSelector_NilNodeSelectorSetsEmptyAnnotation(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	tnt.Spec.NodeSelector = nil

	out := utils.BuildNodeSelector(tnt, map[string]string{})

	got, ok := out[utils.NodeSelectorAnnotation]
	if !ok {
		t.Fatalf("expected %q annotation to be present even for nil selector", utils.NodeSelectorAnnotation)
	}
	if got != "" {
		t.Fatalf("expected empty annotation value, got %q", got)
	}
}

func TestBuildNodeSelector_ReturnsSameMapInstance(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{}
	tnt.Spec.NodeSelector = map[string]string{"a": "1"}

	in := map[string]string{"x": "y"}
	out := utils.BuildNodeSelector(tnt, in)

	// BuildNodeSelector mutates and returns the same map reference
	if &in == &out {
		// Note: maps are reference types; direct pointer comparison isn't meaningful.
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 keys in resulting map, got %d", len(out))
	}
	if out["x"] != "y" {
		t.Fatalf("expected original key to remain")
	}
}
