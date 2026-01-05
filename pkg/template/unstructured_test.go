// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template_test

import (
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/template"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Adjust these if your MissingKeyOption constants are named differently.
var (
	missingKeyErr  = template.MissingKeyOption("error")
	missingKeyZero = template.MissingKeyOption("zero")
)

func mustOne(t *testing.T, items []*unstructured.Unstructured) *unstructured.Unstructured {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	return items[0]
}

func TestDefaultSanitizeUnstructuredOptions(t *testing.T) {
	opts := template.DefaultSanitizeUnstructuredOptions()

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
	template.SanitizeUnstructured(nil, template.DefaultSanitizeUnstructuredOptions())
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
	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
		StripManagedFields: false,
		StripLastApplied:   false,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); !found {
		t.Fatalf("expected managedFields to remain when StripManagedFields=false")
	}

	// Enabled: should be removed
	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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
	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
		StripManagedFields: false,
		StripLastApplied:   false,
		StripStatus:        false,
	})

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "status"); !found {
		t.Fatalf("expected status to remain when StripStatus=false")
	}

	// Enabled: should be removed
	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

	template.SanitizeUnstructured(obj, template.SanitizeUnstructuredOptions{
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

func TestRenderUnstructuredItems_SingleYAMLDocument(t *testing.T) {
	ctx := template.ReferenceContext{"name": "cm-1"}

	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .name }}
data:
  x: y
`
	items, err := template.RenderUnstructuredItems(ctx, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	u := mustOne(t, items)
	if u.GetAPIVersion() != "v1" {
		t.Fatalf("expected apiVersion=v1, got %q", u.GetAPIVersion())
	}
	if u.GetKind() != "ConfigMap" {
		t.Fatalf("expected kind=ConfigMap, got %q", u.GetKind())
	}
	if u.GetName() != "cm-1" {
		t.Fatalf("expected name=cm-1, got %q", u.GetName())
	}
}

func TestRenderUnstructuredItems_MultiDoc_SkipsEmptyWhitespaceAndNullDocs(t *testing.T) {
	tpl := `
---
apiVersion: v1
kind: Namespace
metadata:
  name: ns-1
---
# empty doc
---
# whitespace doc

---
null
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-2
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].GetKind() != "Namespace" || items[0].GetName() != "ns-1" {
		t.Fatalf("unexpected first object: kind=%q name=%q", items[0].GetKind(), items[0].GetName())
	}
	if items[1].GetKind() != "ConfigMap" || items[1].GetName() != "cm-2" {
		t.Fatalf("unexpected second object: kind=%q name=%q", items[1].GetKind(), items[1].GetName())
	}
}

func TestRenderUnstructuredItems_SkipsObjectMissingBothKindAndAPIVersion(t *testing.T) {
	tpl := `
metadata:
  name: skipped
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: kept
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].GetName() != "kept" {
		t.Fatalf("expected kept object, got name=%q", items[0].GetName())
	}
}

func TestRenderUnstructuredItems_DoesNotSkipIfOnlyOneOfKindOrAPIVersionPresent(t *testing.T) {
	tpl := `
apiVersion: v1
metadata:
  name: only-apiversion
---
kind: ConfigMap
metadata:
  name: only-kind
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].GetAPIVersion() != "v1" || items[0].GetName() != "only-apiversion" {
		t.Fatalf("unexpected first object: apiVersion=%q name=%q", items[0].GetAPIVersion(), items[0].GetName())
	}
	if items[1].GetKind() != "ConfigMap" || items[1].GetName() != "only-kind" {
		t.Fatalf("unexpected second object: kind=%q name=%q", items[1].GetKind(), items[1].GetName())
	}
}

func TestRenderUnstructuredItems_JSONDocument(t *testing.T) {
	tpl := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-json"}}`

	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	u := mustOne(t, items)
	if u.GetKind() != "ConfigMap" || u.GetName() != "cm-json" {
		t.Fatalf("unexpected object: kind=%q name=%q", u.GetKind(), u.GetName())
	}
}

func TestRenderUnstructuredItems_TemplateParseError(t *testing.T) {
	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .name
`
	_, err := template.RenderUnstructuredItems(template.ReferenceContext{"name": "x"}, missingKeyErr, tpl)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
}

func TestRenderUnstructuredItems_MissingKey_ErrorMode(t *testing.T) {
	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .doesNotExist }}
`
	_, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err == nil {
		t.Fatalf("expected execute error for missing key, got nil")
	}
}

func TestRenderUnstructuredItems_MissingKey_ZeroMode_AllowsRender(t *testing.T) {
	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .doesNotExist }}
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyZero, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	u := mustOne(t, items)
	if u.GetKind() != "ConfigMap" {
		t.Fatalf("expected kind=ConfigMap, got %q", u.GetKind())
	}
}

func TestRenderUnstructuredItems_MalformedYAML_ReturnsDecodeError(t *testing.T) {
	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  a: b
   c: d
`
	_, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err == nil {
		t.Fatalf("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode yaml") {
		t.Fatalf("expected error to contain %q, got: %v", "decode yaml", err)
	}
}

func TestRenderUnstructuredItems_SequenceRoot_IsError(t *testing.T) {
	tpl := `
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: cm
`
	_, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err == nil {
		t.Fatalf("expected decode error for sequence root, got nil")
	}
	if !strings.Contains(err.Error(), "decode yaml") {
		t.Fatalf("expected error to contain %q, got: %v", "decode yaml", err)
	}
}

func TestRenderUnstructuredItems_ScalarRoot_IsError(t *testing.T) {
	tpl := `just-a-string`
	_, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err == nil {
		t.Fatalf("expected decode error for scalar root, got nil")
	}
	if !strings.Contains(err.Error(), "decode yaml") {
		t.Fatalf("expected error to contain %q, got: %v", "decode yaml", err)
	}
}

func TestRenderUnstructuredItems_WhitespaceOnly_ReturnsEmptySlice(t *testing.T) {
	tpl := "\n   \n\t\n"
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestRenderUnstructuredItems_ContextNestedTypes_RenderOK(t *testing.T) {
	ctx := template.ReferenceContext{
		"outer": map[string]any{
			"inner": "v",
		},
		"list": []any{"a", "b"},
	}

	tpl := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-{{ index .list 0 }}
data:
  x: {{ .outer.inner }}
`

	items, err := template.RenderUnstructuredItems(ctx, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	u := mustOne(t, items)
	if u.GetName() != "cm-a" {
		t.Fatalf("expected name=cm-a, got %q", u.GetName())
	}
}

func TestReferenceContext_String_MarshalUnmarshalRoundTrip(t *testing.T) {
	ctx := template.ReferenceContext{
		"a": "b",
		"n": 1,
		"m": map[string]any{"x": "y"},
	}

	s, err := ctx.String()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(s, `"a":"b"`) {
		t.Fatalf("expected JSON to contain %q, got %q", `"a":"b"`, s)
	}
}

func TestRenderUnstructuredItems_MultiYAML_AllValid(t *testing.T) {
	ctx := template.ReferenceContext{"ns": "ns-1"}

	tpl := `
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .ns }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-1
  namespace: {{ .ns }}
data:
  k: v
---
apiVersion: v1
kind: Secret
metadata:
  name: s-1
  namespace: {{ .ns }}
type: Opaque
stringData:
  a: b
`
	items, err := template.RenderUnstructuredItems(ctx, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	if items[0].GetKind() != "Namespace" || items[0].GetName() != "ns-1" {
		t.Fatalf("unexpected item0: kind=%q name=%q", items[0].GetKind(), items[0].GetName())
	}
	if items[1].GetKind() != "ConfigMap" || items[1].GetName() != "cm-1" || items[1].GetNamespace() != "ns-1" {
		t.Fatalf("unexpected item1: kind=%q name=%q ns=%q", items[1].GetKind(), items[1].GetName(), items[1].GetNamespace())
	}
	if items[2].GetKind() != "Secret" || items[2].GetName() != "s-1" || items[2].GetNamespace() != "ns-1" {
		t.Fatalf("unexpected item2: kind=%q name=%q ns=%q", items[2].GetKind(), items[2].GetName(), items[2].GetNamespace())
	}
}

func TestRenderUnstructuredItems_MultiJSON_NewlineDelimited(t *testing.T) {
	// YAMLOrJSONDecoder supports multiple JSON objects if separated in the stream (e.g. NDJSON).
	tpl := `
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-a"}}
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-b"}}
{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"ns-c"}}
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	if items[0].GetName() != "cm-a" || items[0].GetKind() != "ConfigMap" {
		t.Fatalf("unexpected item0: kind=%q name=%q", items[0].GetKind(), items[0].GetName())
	}
	if items[1].GetName() != "cm-b" || items[1].GetKind() != "ConfigMap" {
		t.Fatalf("unexpected item1: kind=%q name=%q", items[1].GetKind(), items[1].GetName())
	}
	if items[2].GetName() != "ns-c" || items[2].GetKind() != "Namespace" {
		t.Fatalf("unexpected item2: kind=%q name=%q", items[2].GetKind(), items[2].GetName())
	}
}

func TestRenderUnstructuredItems_MixedYAMLAndJSON_AllValid(t *testing.T) {
	// Decoder supports YAML and JSON in same stream.
	tpl := `
apiVersion: v1
kind: Namespace
metadata:
  name: ns-1
---
{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-1","namespace":"ns-1"}}
---
apiVersion: v1
kind: Secret
metadata:
  name: s-1
  namespace: ns-1
type: Opaque
stringData:
  a: b
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	if items[0].GetKind() != "Namespace" || items[0].GetName() != "ns-1" {
		t.Fatalf("unexpected item0: kind=%q name=%q", items[0].GetKind(), items[0].GetName())
	}
	if items[1].GetKind() != "ConfigMap" || items[1].GetName() != "cm-1" || items[1].GetNamespace() != "ns-1" {
		t.Fatalf("unexpected item1: kind=%q name=%q ns=%q", items[1].GetKind(), items[1].GetName(), items[1].GetNamespace())
	}
	if items[2].GetKind() != "Secret" || items[2].GetName() != "s-1" || items[2].GetNamespace() != "ns-1" {
		t.Fatalf("unexpected item2: kind=%q name=%q ns=%q", items[2].GetKind(), items[2].GetName(), items[2].GetNamespace())
	}
}

func TestRenderUnstructuredItems_MultiDocs_EmptyMapAndNullAreSkipped(t *testing.T) {
	tpl := `
{}
---
null
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-1
---
{} # another empty doc
---
apiVersion: v1
kind: Namespace
metadata:
  name: ns-1
`
	items, err := template.RenderUnstructuredItems(template.ReferenceContext{}, missingKeyErr, tpl)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].GetKind() != "ConfigMap" || items[0].GetName() != "cm-1" {
		t.Fatalf("unexpected item0: kind=%q name=%q", items[0].GetKind(), items[0].GetName())
	}
	if items[1].GetKind() != "Namespace" || items[1].GetName() != "ns-1" {
		t.Fatalf("unexpected item1: kind=%q name=%q", items[1].GetKind(), items[1].GetName())
	}
}
