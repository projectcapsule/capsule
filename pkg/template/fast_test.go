// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTenant(name string) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func TestTemplateForTenantAndNamespace_ReplacesPlaceholders(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	m := map[string]string{
		"key1": "tenant={{ tenant.name }}, ns={{ namespace }}",
		"key2": "plain-value",
	}

	TemplateForTenantAndNamespace(m, tnt, ns)

	if got := m["key1"]; got != "tenant=tenant-a, ns=ns-1" {
		t.Fatalf("key1: expected %q, got %q", "tenant=tenant-a, ns=ns-1", got)
	}

	if got := m["key2"]; got != "plain-value" {
		t.Fatalf("key2: expected %q to remain unchanged, got %q", "plain-value", got)
	}
}

func TestTemplateForTenantAndNamespace_SkipsValuesWithoutDelimiters(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	// Note: no space after '{{' and before '}}', so the guard should skip it
	m := map[string]string{
		"noTemplate1": "hello {{tenant.name}}",
		"noTemplate2": "namespace {{namespace}}",
	}

	original1 := m["noTemplate1"]
	original2 := m["noTemplate2"]

	TemplateForTenantAndNamespace(m, tnt, ns)

	if got := m["noTemplate1"]; got != original1 {
		t.Fatalf("noTemplate1: expected %q to remain unchanged, got %q", original1, got)
	}
	if got := m["noTemplate2"]; got != original2 {
		t.Fatalf("noTemplate2: expected %q to remain unchanged, got %q", original2, got)
	}
}

func TestTemplateForTenantAndNamespace_MixedKeys(t *testing.T) {
	tnt := newTenant("tenant-x")
	ns := newNamespace("ns-x")

	m := map[string]string{
		"onlyTenant": "T={{ tenant.name }}",
		"onlyNS":     "N={{ namespace }}",
		"none":       "static",
	}

	TemplateForTenantAndNamespace(m, tnt, ns)

	if got := m["onlyTenant"]; got != "T=tenant-x" {
		t.Fatalf("onlyTenant: expected %q, got %q", "T=tenant-x", got)
	}
	if got := m["onlyNS"]; got != "N=ns-x" {
		t.Fatalf("onlyNS: expected %q, got %q", "N=ns-x", got)
	}
	if got := m["none"]; got != "static" {
		t.Fatalf("none: expected %q to remain unchanged, got %q", "static", got)
	}
}

func TestTemplateForTenantAndNamespace_UnknownKeyBecomesEmpty(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	m := map[string]string{
		"unknown": "X={{ unknown.key }}",
	}

	TemplateForTenantAndNamespace(m, tnt, ns)

	// fasttemplate with missing key returns an empty string for that placeholder
	if got := m["unknown"]; got != "X=" {
		t.Fatalf("unknown: expected %q, got %q", "X=", got)
	}
}
