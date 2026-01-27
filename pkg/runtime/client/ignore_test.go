// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client_test

import (
	"reflect"
	"testing"

	"github.com/fluxcd/pkg/apis/kustomize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/runtime/client"
)

func TestIgnoreRule_Matches(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("apps/v1")
	obj.SetKind("Deployment")
	obj.SetNamespace("ns1")
	obj.SetName("my-deploy")
	obj.SetLabels(map[string]string{"app": "demo"})
	obj.SetAnnotations(map[string]string{"a": "b"})

	t.Run("nil receiver matches all", func(t *testing.T) {
		var r *client.IgnoreRule
		if !r.Matches(obj) {
			t.Fatalf("expected true")
		}
	})

	t.Run("nil target matches all", func(t *testing.T) {
		r := &client.IgnoreRule{Paths: []string{"/x"}, Target: nil}
		if !r.Matches(obj) {
			t.Fatalf("expected true")
		}
	})

	t.Run("matches by kind/name/namespace", func(t *testing.T) {
		r := &client.IgnoreRule{
			Paths: []string{"/x"},
			Target: &kustomize.Selector{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Namespace: "ns1",
				Name:      "my-deploy",
			},
		}
		if !r.Matches(obj) {
			t.Fatalf("expected true")
		}
	})

	t.Run("does not match when kind differs", func(t *testing.T) {
		r := &client.IgnoreRule{
			Paths: []string{"/x"},
			Target: &kustomize.Selector{
				Group:     "apps",
				Version:   "v1",
				Kind:      "StatefulSet",
				Namespace: "ns1",
				Name:      "my-deploy",
			},
		}
		if r.Matches(obj) {
			t.Fatalf("expected false")
		}
	})

	t.Run("matches by label selector", func(t *testing.T) {
		r := &client.IgnoreRule{
			Paths: []string{"/x"},
			Target: &kustomize.Selector{
				Group:         "apps",
				Version:       "v1",
				Kind:          "Deployment",
				LabelSelector: "app=demo",
			},
		}
		if !r.Matches(obj) {
			t.Fatalf("expected true")
		}
	})

	t.Run("matches by annotation selector", func(t *testing.T) {
		r := &client.IgnoreRule{
			Paths: []string{"/x"},
			Target: &kustomize.Selector{
				Group:              "apps",
				Version:            "v1",
				Kind:               "Deployment",
				AnnotationSelector: "a=b",
			},
		}
		if !r.Matches(obj) {
			t.Fatalf("expected true")
		}
	})

	t.Run("invalid regex in selector returns false", func(t *testing.T) {
		// jsondiff.NewSelectorRegex treats certain fields as regex; a broken one should error.
		r := &client.IgnoreRule{
			Paths: []string{"/x"},
			Target: &kustomize.Selector{
				Kind: "Deployment",
				Name: "[", // invalid regex
			},
		}
		if r.Matches(obj) {
			t.Fatalf("expected false")
		}
	})
}

func Test_jsonPointerGet(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"app": "demo",
				"a/b": "v",
				"t~k": "v2",
			},
		},
		"spec": map[string]any{
			"list": []any{
				"zero",
				map[string]any{"x": "y"},
			},
		},
	}

	t.Run("root empty pointer", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "")
		if !ok {
			t.Fatalf("expected ok")
		}
		if v == nil {
			t.Fatalf("expected value")
		}
	})

	t.Run("root slash pointer", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/")
		if !ok {
			t.Fatalf("expected ok")
		}
		_, isMap := v.(map[string]any)
		if !isMap {
			t.Fatalf("expected map root")
		}
	})

	t.Run("simple path", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/metadata/labels/app")
		if !ok || v != "demo" {
			t.Fatalf("expected demo, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("escaped slash key ~1", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/metadata/labels/a~1b")
		if !ok || v != "v" {
			t.Fatalf("expected v, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("escaped tilde key ~0", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/metadata/labels/t~0k")
		if !ok || v != "v2" {
			t.Fatalf("expected v2, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("array index", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/spec/list/0")
		if !ok || v != "zero" {
			t.Fatalf("expected zero, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("array index into object", func(t *testing.T) {
		v, ok := client.JsonPointerGet(obj, "/spec/list/1/x")
		if !ok || v != "y" {
			t.Fatalf("expected y, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		_, ok := client.JsonPointerGet(obj, "/metadata/labels/nope")
		if ok {
			t.Fatalf("expected not ok")
		}
	})

	t.Run("bad array index", func(t *testing.T) {
		_, ok := client.JsonPointerGet(obj, "/spec/list/nope")
		if ok {
			t.Fatalf("expected not ok")
		}
	})

	t.Run("out of bounds array index", func(t *testing.T) {
		_, ok := client.JsonPointerGet(obj, "/spec/list/99")
		if ok {
			t.Fatalf("expected not ok")
		}
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, ok := client.JsonPointerGet(obj, "/metadata/labels/app/x")
		if ok {
			t.Fatalf("expected not ok")
		}
	})
}

func Test_jsonPointerSet(t *testing.T) {
	t.Run("set root fails", func(t *testing.T) {
		obj := map[string]any{"a": "b"}
		if err := client.JsonPointerSet(obj, "", "x"); err == nil {
			t.Fatalf("expected error")
		}
		if err := client.JsonPointerSet(obj, "/", "x"); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("set creates intermediate maps", func(t *testing.T) {
		obj := map[string]any{}
		if err := client.JsonPointerSet(obj, "/spec/template/metadata/labels/app", "demo"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		v, ok := client.JsonPointerGet(obj, "/spec/template/metadata/labels/app")
		if !ok || v != "demo" {
			t.Fatalf("expected demo, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("set overwrites non-map intermediate with map", func(t *testing.T) {
		obj := map[string]any{
			"spec": "not-a-map",
		}
		if err := client.JsonPointerSet(obj, "/spec/x", "y"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		v, ok := client.JsonPointerGet(obj, "/spec/x")
		if !ok || v != "y" {
			t.Fatalf("expected y, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("set supports escaped keys", func(t *testing.T) {
		obj := map[string]any{}
		if err := client.JsonPointerSet(obj, "/metadata/labels/a~1b", "v"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		v, ok := client.JsonPointerGet(obj, "/metadata/labels/a~1b")
		if !ok || v != "v" {
			t.Fatalf("expected v, got ok=%v v=%v", ok, v)
		}
	})
}

func Test_jsonPointerDelete(t *testing.T) {
	t.Run("delete root fails", func(t *testing.T) {
		obj := map[string]any{"a": "b"}
		if err := client.JsonPointerDelete(obj, ""); err == nil {
			t.Fatalf("expected error")
		}
		if err := client.JsonPointerDelete(obj, "/"); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("delete existing leaf", func(t *testing.T) {
		obj := map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{
					"app": "demo",
				},
			},
		}
		if err := client.JsonPointerDelete(obj, "/metadata/labels/app"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		_, ok := client.JsonPointerGet(obj, "/metadata/labels/app")
		if ok {
			t.Fatalf("expected deleted")
		}
	})

	t.Run("delete missing path is no-op", func(t *testing.T) {
		obj := map[string]any{
			"metadata": map[string]any{},
		}
		if err := client.JsonPointerDelete(obj, "/metadata/labels/app"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	})

	t.Run("delete stops on non-map intermediate", func(t *testing.T) {
		obj := map[string]any{
			"metadata": "not-a-map",
		}
		if err := client.JsonPointerDelete(obj, "/metadata/labels/app"); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// still unchanged
		if obj["metadata"] != "not-a-map" {
			t.Fatalf("expected unchanged")
		}
	})
}

func Test_preserveIgnoredPaths(t *testing.T) {
	t.Run("copies live value into desired when present", func(t *testing.T) {
		desired := map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{
					"keep": "x",
				},
			},
		}
		live := map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{
					"keep":  "x",
					"other": "y",
				},
			},
		}

		client.PreserveIgnoredPaths(desired, live, []string{"/metadata/labels/other"})

		v, ok := client.JsonPointerGet(desired, "/metadata/labels/other")
		if !ok || v != "y" {
			t.Fatalf("expected preserved value y, got ok=%v v=%v", ok, v)
		}
	})

	t.Run("deletes desired value when missing from live", func(t *testing.T) {
		desired := map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{
					"toDelete": "x",
				},
			},
		}
		live := map[string]any{
			"metadata": map[string]any{
				"labels": map[string]any{},
			},
		}

		client.PreserveIgnoredPaths(desired, live, []string{"/metadata/labels/toDelete"})

		_, ok := client.JsonPointerGet(desired, "/metadata/labels/toDelete")
		if ok {
			t.Fatalf("expected key to be deleted in desired")
		}
	})

	t.Run("handles nested missing parents by creating them on set", func(t *testing.T) {
		desired := map[string]any{}
		live := map[string]any{
			"spec": map[string]any{
				"template": map[string]any{
					"metadata": map[string]any{
						"annotations": map[string]any{
							"a": "b",
						},
					},
				},
			},
		}

		client.PreserveIgnoredPaths(desired, live, []string{"/spec/template/metadata/annotations/a"})

		v, ok := client.JsonPointerGet(desired, "/spec/template/metadata/annotations/a")
		if !ok || v != "b" {
			t.Fatalf("expected b, got ok=%v v=%v", ok, v)
		}
	})
}

func Test_matchIgnorePaths(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("apps/v1")
	obj.SetKind("Deployment")
	obj.SetNamespace("ns1")
	obj.SetName("my-deploy")
	obj.SetLabels(map[string]string{"app": "demo"})

	rules := []client.IgnoreRule{
		{
			Paths: []string{"/a"},
			// nil target => matches all
		},
		{
			Paths: []string{"/b", "/c"},
			Target: &kustomize.Selector{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Namespace: "ns1",
				Name:      "my-deploy",
			},
		},
		{
			Paths: []string{"/nope"},
			Target: &kustomize.Selector{
				Kind: "StatefulSet",
			},
		},
	}

	out := client.MatchIgnorePaths(rules, obj)
	want := []string{"/a", "/b", "/c"}

	if !reflect.DeepEqual(out, want) {
		t.Fatalf("unexpected paths:\nwant=%v\ngot =%v", want, out)
	}
}
