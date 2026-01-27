// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mustOne(t *testing.T, items []*unstructured.Unstructured) *unstructured.Unstructured {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	return items[0]
}

func TestDefaultSanitizeUnstructuredOptions(t *testing.T) {
	opts := sanitize.DefaultSanitizeOptions()

	if !opts.StripManagedFields {
		t.Fatalf("expected StripManagedFields=true")
	}
	if !opts.StripLastApplied {
		t.Fatalf("expected StripLastApplied=true")
	}
	if opts.StripStatus {
		t.Fatalf("expected StripStatus=false")
	}
}

func TestSanitizeUnstructured_NilObject_NoPanic(t *testing.T) {
	// Just ensure it doesn't panic
	sanitize.SanitizeUnstructured(nil, sanitize.DefaultSanitizeOptions())
}

func TestSanitizeUnstructured_StripManagedFields_RemovesOnlyWhenEnabled(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "x",
				"managedFields": []any{
					map[string]any{"manager": "foo"},
				},
			},
		},
	}

	// Disabled: should remain
	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   false,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); !found {
		t.Fatalf("expected managedFields to remain when StripManagedFields=false")
	}

	// Enabled: should be removed
	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: true,
		StripLastApplied:   false,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); found {
		t.Fatalf("expected managedFields to be removed when StripManagedFields=true")
	}
}

func TestSanitizeUnstructured_StripLastApplied_RemovesKeyButKeepsOtherAnnotations(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": "huge",
					"keep": "me",
				},
			},
		},
	}

	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   true,
		StripStatus:        false,
	})

	anns, found, err := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if err != nil {
		t.Fatalf("unexpected error reading annotations: %v", err)
	}
	if !found {
		t.Fatalf("expected annotations to exist")
	}
	if _, ok := anns["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("expected last-applied annotation to be removed")
	}
	if anns["keep"] != "me" {
		t.Fatalf("expected other annotations to be preserved, got: %#v", anns)
	}
}

func TestSanitizeUnstructured_StripLastApplied_RemovesAnnotationsFieldWhenItBecomesEmpty(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": "huge",
				},
			},
		},
	}

	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   true,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "annotations"); found {
		t.Fatalf("expected metadata.annotations to be removed entirely when empty")
	}
}

func TestSanitizeUnstructured_StripLastApplied_NoAnnotations_NoError(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name": "x",
			},
		},
	}

	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   true,
		StripStatus:        false,
	})

	// Nothing to assert besides "doesn't crash" and metadata still present
	if got := obj.GetName(); got != "x" {
		t.Fatalf("expected name to stay unchanged, got %q", got)
	}
}

func TestSanitizeUnstructured_StripLastApplied_AnnotationsNotStringMap_IsIgnored(t *testing.T) {
	// NestedStringMap will return an error if annotations is not a map[string]string
	// and SanitizeUnstructured should ignore it (no crash, no deletion).
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"annotations": []any{"not-a-map"},
			},
		},
	}

	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   true,
		StripStatus:        false,
	})

	// Still present because we ignored on error
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "annotations"); !found {
		t.Fatalf("expected annotations to remain when annotations is malformed and cannot be parsed as string map")
	}
}

func TestSanitizeUnstructured_StripStatus_RemovesStatusOnlyWhenEnabled(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "x"},
			"status": map[string]any{
				"phase": "Active",
			},
		},
	}

	// Disabled: should remain
	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   false,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "status"); !found {
		t.Fatalf("expected status to remain when StripStatus=false")
	}

	// Enabled: should be removed
	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: false,
		StripLastApplied:   false,
		StripStatus:        true,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "status"); found {
		t.Fatalf("expected status to be removed when StripStatus=true")
	}
}

func TestSanitizeUnstructured_AllOptionsEnabled_RemovesAllTargets(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"managedFields": []any{
					map[string]any{"manager": "foo"},
				},
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": "huge",
					"keep": "me",
				},
			},
			"status": map[string]any{"foo": "bar"},
		},
	}

	sanitize.SanitizeUnstructured(obj, sanitize.SanitizeOptions{
		StripManagedFields: true,
		StripLastApplied:   true,
		StripStatus:        true,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); found {
		t.Fatalf("expected managedFields removed")
	}

	anns, found, err := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if err != nil {
		t.Fatalf("unexpected error reading annotations: %v", err)
	}
	if !found {
		t.Fatalf("expected annotations to still exist because 'keep' should remain")
	}
	if _, ok := anns["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("expected last-applied removed")
	}
	if anns["keep"] != "me" {
		t.Fatalf("expected keep annotation preserved, got %#v", anns)
	}

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "status"); found {
		t.Fatalf("expected status removed")
	}
}
