// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client_test

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/runtime/client"
)

func TestAddLabelsPatch_MapInput(t *testing.T) {
	t.Run("nil labels => add op", func(t *testing.T) {
		var labels map[string]string // nil

		patches := client.AddLabelsPatch(labels, map[string]string{
			"a": "1",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/labels/a", Value: "1"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("existing key same value => no patch", func(t *testing.T) {
		labels := map[string]string{"a": "1"}

		patches := client.AddLabelsPatch(labels, map[string]string{
			"a": "1",
		})

		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("existing key different value => replace op", func(t *testing.T) {
		labels := map[string]string{"a": "1"}

		patches := client.AddLabelsPatch(labels, map[string]string{
			"a": "2",
		})

		want := []client.JSONPatch{
			{Operation: "replace", Path: "/metadata/labels/a", Value: "2"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("missing key => add op", func(t *testing.T) {
		labels := map[string]string{"a": "1"}

		patches := client.AddLabelsPatch(labels, map[string]string{
			"b": "2",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/labels/b", Value: "2"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("key contains slash => path escaped with ~1", func(t *testing.T) {
		labels := map[string]string{}

		patches := client.AddLabelsPatch(labels, map[string]string{
			"projectcapsule.dev/tenant": "wind",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/labels/projectcapsule.dev~1tenant", Value: "wind"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})
}

func TestAddAnnotationsPatch_MapInput(t *testing.T) {
	t.Run("nil annotations => add op", func(t *testing.T) {
		var annotations map[string]string // nil

		patches := client.AddAnnotationsPatch(annotations, map[string]string{
			"a": "1",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/annotations/a", Value: "1"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("existing key same value => no patch", func(t *testing.T) {
		annotations := map[string]string{"a": "1"}

		patches := client.AddAnnotationsPatch(annotations, map[string]string{
			"a": "1",
		})

		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("existing key different value => replace op", func(t *testing.T) {
		annotations := map[string]string{"a": "1"}

		patches := client.AddAnnotationsPatch(annotations, map[string]string{
			"a": "2",
		})

		want := []client.JSONPatch{
			{Operation: "replace", Path: "/metadata/annotations/a", Value: "2"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("missing key => add op", func(t *testing.T) {
		annotations := map[string]string{"a": "1"}

		patches := client.AddAnnotationsPatch(annotations, map[string]string{
			"b": "2",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/annotations/b", Value: "2"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("key contains slash => path escaped with ~1", func(t *testing.T) {
		annotations := map[string]string{}

		patches := client.AddAnnotationsPatch(annotations, map[string]string{
			"example.com/foo": "bar",
		})

		want := []client.JSONPatch{
			{Operation: "add", Path: "/metadata/annotations/example.com~1foo", Value: "bar"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})
}

func TestPatchRemoveLabels_MapInput(t *testing.T) {
	t.Run("nil labels => no patch", func(t *testing.T) {
		var labels map[string]string // nil

		patches := client.PatchRemoveLabels(labels, []string{"a"})
		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("existing key => remove patch", func(t *testing.T) {
		labels := map[string]string{"a": "1"}

		patches := client.PatchRemoveLabels(labels, []string{"a"})

		want := []client.JSONPatch{
			{Operation: "remove", Path: "/metadata/labels/a"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("missing key => no patch", func(t *testing.T) {
		labels := map[string]string{"a": "1"}

		patches := client.PatchRemoveLabels(labels, []string{"nope"})
		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("key contains slash => path escaped with ~1", func(t *testing.T) {
		labels := map[string]string{"projectcapsule.dev/tenant": "wind"}

		patches := client.PatchRemoveLabels(labels, []string{"projectcapsule.dev/tenant"})

		want := []client.JSONPatch{
			{Operation: "remove", Path: "/metadata/labels/projectcapsule.dev~1tenant"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})
}

func TestPatchRemoveAnnotations_MapInput(t *testing.T) {
	t.Run("nil annotations => no patch", func(t *testing.T) {
		var annotations map[string]string // nil

		patches := client.PatchRemoveAnnotations(annotations, []string{"a"})
		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("existing key => remove patch", func(t *testing.T) {
		annotations := map[string]string{"a": "1"}

		patches := client.PatchRemoveAnnotations(annotations, []string{"a"})

		want := []client.JSONPatch{
			{Operation: "remove", Path: "/metadata/annotations/a"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})

	t.Run("missing key => no patch", func(t *testing.T) {
		annotations := map[string]string{"a": "1"}

		patches := client.PatchRemoveAnnotations(annotations, []string{"nope"})
		if len(patches) != 0 {
			t.Fatalf("expected no patches, got %v", patches)
		}
	})

	t.Run("key contains slash => path escaped with ~1", func(t *testing.T) {
		annotations := map[string]string{"example.com/foo": "bar"}

		patches := client.PatchRemoveAnnotations(annotations, []string{"example.com/foo"})

		want := []client.JSONPatch{
			{Operation: "remove", Path: "/metadata/annotations/example.com~1foo"},
		}

		if !reflect.DeepEqual(patches, want) {
			t.Fatalf("unexpected patches\nwant=%v\ngot =%v", want, patches)
		}
	})
}

func TestRemoveOwnerReferencePatch(t *testing.T) {
	t.Parallel()

	mkRef := func(name, uid string, controller, block bool) metav1.OwnerReference {
		c := controller
		b := block
		return metav1.OwnerReference{
			APIVersion:         "v1",
			Kind:               "ConfigMap",
			Name:               name,
			UID:                types.UID(uid),
			Controller:         &c,
			BlockOwnerDeletion: &b,
		}
	}

	t.Run("nil toRemove returns nil", func(t *testing.T) {
		t.Parallel()

		refs := []metav1.OwnerReference{mkRef("a", "uid-a", true, true)}
		got := client.RemoveOwnerReferencePatch(refs, nil)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("empty ownerRefs returns nil", func(t *testing.T) {
		t.Parallel()

		toRemove := mkRef("a", "uid-a", true, true)
		got := client.RemoveOwnerReferencePatch(nil, &toRemove)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}

		got = client.RemoveOwnerReferencePatch([]metav1.OwnerReference{}, &toRemove)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("no matching ownerReference returns nil", func(t *testing.T) {
		t.Parallel()

		refs := []metav1.OwnerReference{
			mkRef("a", "uid-a", true, true),
			mkRef("b", "uid-b", false, false),
		}
		// Different UID and name/kind => should not match
		toRemove := mkRef("c", "uid-c", true, true)

		got := client.RemoveOwnerReferencePatch(refs, &toRemove)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("match in middle returns single remove patch with correct index", func(t *testing.T) {
		t.Parallel()

		refs := []metav1.OwnerReference{
			mkRef("a", "uid-a", true, true),
			mkRef("b", "uid-b", false, false),
			mkRef("c", "uid-c", true, false),
		}

		// Make toRemove identical to refs[1] so LooseOwnerReferenceEqual is true.
		toRemove := refs[1]

		got := client.RemoveOwnerReferencePatch(refs, &toRemove)
		if got == nil {
			t.Fatalf("expected patches, got nil")
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 patch, got %d: %#v", len(got), got)
		}
		if got[0].Operation != "remove" {
			t.Fatalf("expected op=remove, got %q", got[0].Operation)
		}
		wantPath := "/metadata/ownerReferences/1"
		if got[0].Path != wantPath {
			t.Fatalf("expected path=%q, got %q", wantPath, got[0].Path)
		}
	})

	t.Run("match first occurrence only", func(t *testing.T) {
		t.Parallel()

		// Duplicate entries (shouldn't happen, but function breaks on first match).
		ref := mkRef("dup", "uid-dup", true, true)
		refs := []metav1.OwnerReference{ref, ref}

		toRemove := ref
		got := client.RemoveOwnerReferencePatch(refs, &toRemove)

		if got == nil || len(got) != 1 {
			t.Fatalf("expected 1 patch, got %#v", got)
		}
		wantPath := "/metadata/ownerReferences/0"
		if got[0].Path != wantPath {
			t.Fatalf("expected path=%q, got %q", wantPath, got[0].Path)
		}
	})

	t.Run("single ownerRef match returns remove element patch AND remove field patch", func(t *testing.T) {
		t.Parallel()

		only := mkRef("only", "uid-only", true, true)
		refs := []metav1.OwnerReference{only}

		toRemove := only
		got := client.RemoveOwnerReferencePatch(refs, &toRemove)
		if got == nil {
			t.Fatalf("expected patches, got nil")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 patches, got %d: %#v", len(got), got)
		}

		if got[0].Operation != "remove" || got[0].Path != "/metadata/ownerReferences/0" {
			t.Fatalf("unexpected first patch: %#v", got[0])
		}
		if got[1].Operation != "remove" || got[1].Path != "/metadata/ownerReferences" {
			t.Fatalf("unexpected second patch: %#v", got[1])
		}
	})

	t.Run("index in path is correct for each position", func(t *testing.T) {
		t.Parallel()

		refs := []metav1.OwnerReference{
			mkRef("a", "uid-a", true, true),
			mkRef("b", "uid-b", true, true),
			mkRef("c", "uid-c", true, true),
		}

		for i := range refs {
			i := i
			t.Run(fmt.Sprintf("match index %d", i), func(t *testing.T) {
				t.Parallel()

				toRemove := refs[i]
				got := client.RemoveOwnerReferencePatch(refs, &toRemove)
				if got == nil || len(got) != 1 {
					t.Fatalf("expected 1 patch, got %#v", got)
				}
				wantPath := fmt.Sprintf("/metadata/ownerReferences/%d", i)
				if got[0].Path != wantPath {
					t.Fatalf("expected path=%q, got %q", wantPath, got[0].Path)
				}
			})
		}
	})
}
