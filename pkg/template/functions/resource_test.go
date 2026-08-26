// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import (
	"strings"
	"testing"
)

func TestGetResourceByName(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		templateResource("team-a", "tenant-management"),
		templateResource("team-a", "tenant-settings"),
	}

	resource, err := getResourceByName("tenant-management", resources)
	if err != nil {
		t.Fatalf("getResourceByName() error = %v", err)
	}
	if resource["data"].(map[string]any)["team"] != "team-a" {
		t.Fatalf("getResourceByName() = %#v", resource)
	}

	missing, err := getResourceByName("missing", resources)
	if err != nil {
		t.Fatalf("getResourceByName(missing) error = %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("getResourceByName(missing) = %#v, want empty map", missing)
	}
}

func TestMustGetResourceByName(t *testing.T) {
	t.Parallel()

	_, err := mustGetResourceByName("missing", []map[string]any{
		templateResource("team-a", "tenant-management"),
	})
	if err == nil || !strings.Contains(err.Error(), `metadata.name "missing" was not found`) {
		t.Fatalf("mustGetResourceByName() error = %v", err)
	}
}

func TestGetResourceByNameRejectsAmbiguousMatch(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		templateResource("team-a", "tenant-management"),
		templateResource("team-b", "tenant-management"),
	}

	_, err := getResourceByName("tenant-management", resources)
	if err == nil || !strings.Contains(err.Error(), "multiple resources match") {
		t.Fatalf("getResourceByName() error = %v", err)
	}
}

func TestGetResourceByNamespacedName(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		templateResource("team-a", "tenant-management"),
		templateResource("team-b", "tenant-management"),
	}

	resource, err := getResourceByNamespacedName("team-b", "tenant-management", resources)
	if err != nil {
		t.Fatalf("getResourceByNamespacedName() error = %v", err)
	}
	if resource["data"].(map[string]any)["team"] != "team-b" {
		t.Fatalf("getResourceByNamespacedName() = %#v", resource)
	}

	clusterScoped := templateResource("", "shared")
	delete(clusterScoped["metadata"].(map[string]any), "namespace")

	resource, err = getResourceByNamespacedName("", "shared", []map[string]any{clusterScoped})
	if err != nil {
		t.Fatalf("getResourceByNamespacedName(cluster-scoped) error = %v", err)
	}
	if resource["metadata"].(map[string]any)["name"] != "shared" {
		t.Fatalf("getResourceByNamespacedName(cluster-scoped) = %#v", resource)
	}
}

func TestGetResourceSupportsJSONRoundTrippedContext(t *testing.T) {
	t.Parallel()

	resources := []any{
		templateResource("team-a", "tenant-management"),
	}

	resource, err := getResourceByName("tenant-management", resources)
	if err != nil {
		t.Fatalf("getResourceByName() error = %v", err)
	}
	if resource["data"].(map[string]any)["team"] != "team-a" {
		t.Fatalf("getResourceByName() = %#v", resource)
	}
}

func TestGetResourceRejectsInvalidContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources any
		wantErr   string
	}{
		{
			name:      "not a list",
			resources: map[string]any{},
			wantErr:   "resources must be a list of objects",
		},
		{
			name:      "list item is not an object",
			resources: []any{"invalid"},
			wantErr:   "resource at index 0 must be an object",
		},
		{
			name:      "missing metadata",
			resources: []map[string]any{{}},
			wantErr:   "resource at index 0 has invalid or missing metadata",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := getResourceByName("tenant-management", test.resources)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("getResourceByName() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func templateResource(namespace, name string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"namespace": namespace,
			"name":      name,
		},
		"data": map[string]any{
			"team": namespace,
		},
	}
}
