// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template_test

import (
	"reflect"
	"strings"
	"testing"

	tpl "github.com/projectcapsule/capsule/pkg/template"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAllowedNamespaceMetadataTemplatesString(t *testing.T) {
	t.Parallel()

	if got := tpl.AllowedNamespaceMetadataTemplatesString(); got != "{{namespace}}, {{tenant.name}}" {
		t.Fatalf("AllowedNamespaceMetadataTemplatesString() = %q", got)
	}
}

func TestContainsFastTemplateSyntax(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "plain", value: "tenant-a", want: false},
		{name: "opening", value: "tenant-{{ namespace", want: true},
		{name: "closing", value: "tenant-namespace }}", want: true},
		{name: "complete", value: "tenant-{{ namespace }}", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tpl.ContainsFastTemplateSyntax(tt.value); got != tt.want {
				t.Fatalf("ContainsFastTemplateSyntax(%q) = %t, want %t", tt.value, got, tt.want)
			}
		})
	}
}

func TestValidateAllowedTemplatesOnly(t *testing.T) {
	t.Parallel()

	if errs := tpl.ValidateAllowedTemplatesOnly("metadata.name", "team-{{ namespace }}-{{ tenant.name }}"); len(errs) > 0 {
		t.Fatalf("ValidateAllowedTemplatesOnly() errors = %#v, want none", errs)
	}

	if errs := tpl.ValidateAllowedTemplatesOnly("metadata.name", "team-{{ owner }}"); len(errs) != 1 ||
		!strings.Contains(errs[0], "unsupported template") ||
		!strings.Contains(errs[0], "{{namespace}}, {{tenant.name}}") {
		t.Fatalf("unsupported template errors = %#v", errs)
	}

	for _, value := range []string{"team-{{ namespace", "team-{{ namespace }}-}}"} {
		if errs := tpl.ValidateAllowedTemplatesOnly("metadata.name", value); len(errs) != 1 ||
			!strings.Contains(errs[0], "malformed template") {
			t.Fatalf("malformed template %q errors = %#v", value, errs)
		}
	}
}

func TestValidateKubernetesStringOrAllowedTemplates(t *testing.T) {
	t.Parallel()

	validator := func(value string) []string {
		if value == "team-template" || value == "team-template-template" {
			return nil
		}

		return []string{"invalid value"}
	}

	if errs := tpl.ValidateKubernetesStringOrAllowedTemplates("metadata.name", "team-{{ namespace }}", validator); len(errs) > 0 {
		t.Fatalf("ValidateKubernetesStringOrAllowedTemplates() errors = %#v, want none", errs)
	}

	got := tpl.ValidateKubernetesStringOrAllowedTemplates("metadata.name", "bad", validator)
	if want := []string{"metadata.name: invalid value"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ValidateKubernetesStringOrAllowedTemplates() = %#v, want %#v", got, want)
	}
}

func TestSelectorRequiresTemplating(t *testing.T) {
	t.Parallel()

	if tpl.SelectorRequiresTemplating(nil) {
		t.Fatalf("SelectorRequiresTemplating(nil) = true, want false")
	}

	for _, tt := range []struct {
		name string
		sel  *metav1.LabelSelector
	}{
		{
			name: "match label key",
			sel:  &metav1.LabelSelector{MatchLabels: map[string]string{"{{ namespace }}": "prod"}},
		},
		{
			name: "match label value",
			sel:  &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "{{ tenant.name }}"}},
		},
		{
			name: "expression key",
			sel: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "{{ namespace }}", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"}},
			}},
		},
		{
			name: "expression value",
			sel: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "environment", Operator: metav1.LabelSelectorOpIn, Values: []string{"{{ tenant.name }}"}},
			}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !tpl.SelectorRequiresTemplating(tt.sel) {
				t.Fatalf("SelectorRequiresTemplating() = false, want true")
			}
		})
	}

	plain := &metav1.LabelSelector{
		MatchLabels: map[string]string{"environment": "prod"},
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"frontend"}},
		},
	}
	if tpl.SelectorRequiresTemplating(plain) {
		t.Fatalf("SelectorRequiresTemplating(plain) = true, want false")
	}
}
