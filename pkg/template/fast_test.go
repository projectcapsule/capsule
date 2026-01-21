// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template_test

import (
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tpl "github.com/projectcapsule/capsule/pkg/template"
)

func TestRequiresFastTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "no braces",
			input:    "plain text with no template markers",
			expected: false,
		},
		{
			name:     "only opening braces",
			input:    "value with {{ but no closing",
			expected: true,
		},
		{
			name:     "only closing braces",
			input:    "value with }} but no opening",
			expected: true,
		},
		{
			name:     "proper template expression",
			input:    "hello {{ .Name }}",
			expected: true,
		},
		{
			name:     "multiple template expressions",
			input:    "{{ .A }} and {{ .B }}",
			expected: true,
		},
		{
			name:     "braces without spaces",
			input:    "{{.Value}}",
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "only opening and closing braces but separated",
			input:    "text {{ middle }} end",
			expected: true,
		},
		{
			name:     "single braces not considered template",
			input:    "{ value }",
			expected: false,
		},
		{
			name:     "nested braces",
			input:    "{{ {{ .Nested }} }}",
			expected: true,
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tpl.RequiresFastTemplate(tt.input)
			if got != tt.expected {
				t.Fatalf(
					"RequiresFastTemplate(%q) = %v, expected %v",
					tt.input,
					got,
					tt.expected,
				)
			}
		})
	}
}

func TestTemplateForTenantAndNamespace_ReplacesPlaceholders(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	got := tpl.FastTemplate(
		"tenant={{tenant.name}}, ns={{namespace}}",
		tplContext,
	)

	want := "tenant=tenant-a, ns=ns-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespace_ReplacesPlaceholdersSpaces(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	got := tpl.FastTemplate(
		"tenant={{ tenant.name }}, ns={{ namespace }}",
		tplContext,
	)

	want := "tenant=tenant-a, ns=ns-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespace_OnlyTenant(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-x",
		"namespace":   "ns-y",
	}

	got := tpl.FastTemplate("T={{tenant.name}}", tplContext)
	want := "T=tenant-x"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespace_OnlyNamespace(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-x",
		"namespace":   "ns-y",
	}

	got := tpl.FastTemplate("N={{namespace}}", tplContext)
	want := "N=ns-y"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespace_NoDelimiters_ReturnsInput(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	in := "plain-value-without-templates"
	got := tpl.FastTemplate(in, tplContext)

	if got != in {
		t.Fatalf("expected %q, got %q", in, got)
	}
}

func TestTemplateForTenantAndNamespace_UnknownKeyBecomesEmpty(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	got := tpl.FastTemplate("X={{unknown.key}}", tplContext)
	want := "X="

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespaceMap_ReplacesPlaceholders(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	orig := map[string]string{
		"key1": "tenant={{tenant.name}}, ns={{namespace}}",
		"key2": "plain-value",
	}

	out := tpl.FastTemplateMap(orig, tplContext)

	// output is templated
	if got := out["key1"]; got != "tenant=tenant-a, ns=ns-1" {
		t.Fatalf("key1: expected %q, got %q", "tenant=tenant-a, ns=ns-1", got)
	}
	if got := out["key2"]; got != "plain-value" {
		t.Fatalf("key2: expected %q, got %q", "plain-value", got)
	}

	// input map must remain unchanged (new behavior)
	if got := orig["key1"]; got != "tenant={{tenant.name}}, ns={{namespace}}" {
		t.Fatalf("input map must not be mutated; key1 got %q", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_ReplacesPlaceholdersSpaces(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	orig := map[string]string{
		"key1": "tenant={{ tenant.name }}, ns={{ namespace }}",
		"key2": "plain-value",
	}

	out := tpl.FastTemplateMap(orig, tplContext)

	if got := out["key1"]; got != "tenant=tenant-a, ns=ns-1" {
		t.Fatalf("key1: expected %q, got %q", "tenant=tenant-a, ns=ns-1", got)
	}
	if got := out["key2"]; got != "plain-value" {
		t.Fatalf("key2: expected %q, got %q", "plain-value", got)
	}

	// input map must remain unchanged
	if got := orig["key1"]; got != "tenant={{ tenant.name }}, ns={{ namespace }}" {
		t.Fatalf("input map must not be mutated; key1 got %q", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_TransformsValuesWithDelimiters(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	orig := map[string]string{
		"t1": "hello {{tenant.name}}",
		"t2": "namespace {{namespace}}",
		"t3": "static",
	}

	out := tpl.FastTemplateMap(orig, tplContext)

	if got := out["t1"]; got != "hello tenant-a" {
		t.Fatalf("t1: expected %q, got %q", "hello tenant-a", got)
	}
	if got := out["t2"]; got != "namespace ns-1" {
		t.Fatalf("t2: expected %q, got %q", "namespace ns-1", got)
	}
	if got := out["t3"]; got != "static" {
		t.Fatalf("t3: expected %q, got %q", "static", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_MixedKeys(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-x",
		"namespace":   "ns-x",
	}

	orig := map[string]string{
		"onlyTenant": "T={{ tenant.name }}",
		"onlyNS":     "N={{ namespace }}",
		"none":       "static",
	}

	out := tpl.FastTemplateMap(orig, tplContext)

	if got := out["onlyTenant"]; got != "T=tenant-x" {
		t.Fatalf("onlyTenant: expected %q, got %q", "T=tenant-x", got)
	}
	if got := out["onlyNS"]; got != "N=ns-x" {
		t.Fatalf("onlyNS: expected %q, got %q", "N=ns-x", got)
	}
	if got := out["none"]; got != "static" {
		t.Fatalf("none: expected %q, got %q", "static", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_UnknownKeyBecomesEmpty(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	orig := map[string]string{
		"unknown": "X={{ unknown.key }}",
	}

	out := tpl.FastTemplateMap(orig, tplContext)

	if got := out["unknown"]; got != "X=" {
		t.Fatalf("unknown: expected %q, got %q", "X=", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_EmptyOrNilInput(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	// nil map
	outNil := tpl.FastTemplateMap(nil, tplContext)
	if outNil == nil {
		t.Fatalf("expected non-nil map for nil input")
	}
	if len(outNil) != 0 {
		t.Fatalf("expected empty map for nil input, got %v", outNil)
	}

	// empty map
	outEmpty := tpl.FastTemplateMap(map[string]string{}, tplContext)
	if outEmpty == nil || len(outEmpty) != 0 {
		t.Fatalf("expected empty map, got %v", outEmpty)
	}
}

// Concurrency test: should never panic with "concurrent map writes"
// Run with: go test -race ./...
func TestTemplateForTenantAndNamespaceMap_Concurrency(t *testing.T) {
	tplContext := map[string]string{
		"tenant.name": "tenant-a",
		"namespace":   "ns-1",
	}

	// Shared input map across goroutines (this used to be unsafe if the function mutated in-place)
	shared := map[string]string{
		"k1": "tenant={{tenant.name}}",
		"k2": "ns={{namespace}}",
		"k3": "static",
	}

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				out := tpl.FastTemplateMap(shared, tplContext)

				// sanity checks
				if out["k1"] != "tenant=tenant-a" {
					t.Errorf("unexpected k1: %q", out["k1"])
					return
				}
				if out["k2"] != "ns=ns-1" {
					t.Errorf("unexpected k2: %q", out["k2"])
					return
				}
				if out["k3"] != "static" {
					t.Errorf("unexpected k3: %q", out["k3"])
					return
				}
			}
		}()
	}

	wg.Wait()

	// verify input map was not mutated
	if shared["k1"] != "tenant={{tenant.name}}" {
		t.Fatalf("input map mutated under concurrency: k1=%q", shared["k1"])
	}
	if shared["k2"] != "ns={{namespace}}" {
		t.Fatalf("input map mutated under concurrency: k2=%q", shared["k2"])
	}
}

func TestFastTemplateLabelSelector(t *testing.T) {
	t.Parallel()

	t.Run("nil selector returns nil, nil", func(t *testing.T) {
		t.Parallel()

		got, err := tpl.FastTemplateLabelSelector(nil, map[string]string{"x": "y"})
		if err != nil {
			t.Fatalf("expected err=nil, got %v", err)
		}
		if got != nil {
			t.Fatalf("expected selector=nil, got %#v", got)
		}
	})

	t.Run("does not mutate input (deep copy)", func(t *testing.T) {
		t.Parallel()

		in := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"created-by": "{{ controller }}",
			},
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "{{ key }}",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"{{ v1 }}", "{{ v2 }}"},
				},
			},
		}

		ctx := map[string]string{
			"controller": "capsule",
			"key":        "env",
			"v1":         "prod",
			"v2":         "staging",
		}

		orig := in.DeepCopy()

		got, err := tpl.FastTemplateLabelSelector(in, ctx)
		if err != nil {
			t.Fatalf("expected err=nil, got %v", err)
		}
		if got == nil {
			t.Fatalf("expected non-nil selector")
		}

		// Input must remain unchanged
		if in.MatchLabels["created-by"] != orig.MatchLabels["created-by"] {
			t.Fatalf("input was mutated: MatchLabels value changed from %q to %q", orig.MatchLabels["created-by"], in.MatchLabels["created-by"])
		}
		if in.MatchExpressions[0].Key != orig.MatchExpressions[0].Key {
			t.Fatalf("input was mutated: MatchExpressions[0].Key changed from %q to %q", orig.MatchExpressions[0].Key, in.MatchExpressions[0].Key)
		}
		if in.MatchExpressions[0].Values[0] != orig.MatchExpressions[0].Values[0] ||
			in.MatchExpressions[0].Values[1] != orig.MatchExpressions[0].Values[1] {
			t.Fatalf("input was mutated: MatchExpressions[0].Values changed from %#v to %#v", orig.MatchExpressions[0].Values, in.MatchExpressions[0].Values)
		}

		// Output should be templated
		if got.MatchLabels["created-by"] != "capsule" {
			t.Fatalf("expected templated MatchLabels[created-by]=capsule, got %q", got.MatchLabels["created-by"])
		}
		if got.MatchExpressions[0].Key != "env" {
			t.Fatalf("expected templated MatchExpressions[0].Key=env, got %q", got.MatchExpressions[0].Key)
		}
		if len(got.MatchExpressions[0].Values) != 2 || got.MatchExpressions[0].Values[0] != "prod" || got.MatchExpressions[0].Values[1] != "staging" {
			t.Fatalf("expected templated values [prod staging], got %#v", got.MatchExpressions[0].Values)
		}
	})

	t.Run("templates matchLabels keys and values via FastTemplateMap", func(t *testing.T) {
		t.Parallel()

		in := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"{{ k1 }}": "{{ v1 }}",
				"static":   "{{ v2 }}",
			},
		}

		ctx := map[string]string{
			"k1": "app",
			"v1": "demo",
			"v2": "x",
		}

		got, err := tpl.FastTemplateLabelSelector(in, ctx)
		if err != nil {
			t.Fatalf("expected err=nil, got %v", err)
		}
		if got == nil {
			t.Fatalf("expected non-nil selector")
		}

		if _, ok := got.MatchLabels["app"]; !ok {
			t.Fatalf("expected templated key 'app' to exist; got keys: %#v", got.MatchLabels)
		}
		if got.MatchLabels["app"] != "demo" {
			t.Fatalf("expected MatchLabels[app]=demo, got %q", got.MatchLabels["app"])
		}
		if got.MatchLabels["static"] != "x" {
			t.Fatalf("expected MatchLabels[static]=x, got %q", got.MatchLabels["static"])
		}
	})

	t.Run("templates matchExpressions key and values", func(t *testing.T) {
		t.Parallel()

		in := &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "tier-{{ t }}",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"{{ a }}", "{{ b }}"},
				},
			},
		}

		ctx := map[string]string{"t": "id", "a": "gold", "b": "silver"}

		got, err := tpl.FastTemplateLabelSelector(in, ctx)
		if err != nil {
			t.Fatalf("expected err=nil, got %v", err)
		}

		if got.MatchExpressions[0].Key != "tier-id" {
			t.Fatalf("expected key=tier-id, got %q", got.MatchExpressions[0].Key)
		}
		if got.MatchExpressions[0].Values[0] != "gold" || got.MatchExpressions[0].Values[1] != "silver" {
			t.Fatalf("expected values [gold silver], got %#v", got.MatchExpressions[0].Values)
		}
	})

	t.Run("returns error when templating produces invalid selector (empty key)", func(t *testing.T) {
		t.Parallel()

		// After templating, Key becomes empty which is invalid for a selector.
		in := &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "{{ missing }}",
					Operator: metav1.LabelSelectorOpExists,
				},
			},
		}

		got, err := tpl.FastTemplateLabelSelector(in, map[string]string{})
		if err == nil {
			t.Fatalf("expected error, got nil (selector=%#v)", got)
		}
	})

	t.Run("key overwrite risk: two templated keys collapse into one without error", func(t *testing.T) {
		t.Parallel()

		// This test documents current behavior (no collision protection).
		// Both keys template to "app". The resulting map will have a single entry.
		in := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"{{ k1 }}": "v1",
				"{{ k2 }}": "v2",
			},
		}

		ctx := map[string]string{"k1": "app", "k2": "app"}

		got, err := tpl.FastTemplateLabelSelector(in, ctx)
		if err != nil {
			t.Fatalf("expected err=nil, got %v", err)
		}
		if got == nil {
			t.Fatalf("expected non-nil selector")
		}

		// Only one key should remain due to collision overwrite behavior.
		if len(got.MatchLabels) != 1 {
			t.Fatalf("expected 1 key after collision, got %d (%#v)", len(got.MatchLabels), got.MatchLabels)
		}
		if _, ok := got.MatchLabels["app"]; !ok {
			t.Fatalf("expected final key 'app' to exist, got %#v", got.MatchLabels)
		}

		// We intentionally do NOT assert which value wins since map iteration order is randomized.
		// This is exactly the risk you mentioned; the test makes it visible.
	})
}
