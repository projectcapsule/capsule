// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
package tenant_test
=======
package template_test
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go

import (
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
=======
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	tpl "github.com/projectcapsule/capsule/pkg/template"
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go
)

func newTenant(name string) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func newNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func TestTemplateForTenantAndNamespace_ReplacesPlaceholders(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	got := tpl.TemplateForTenantAndNamespace(
		"tenant={{tenant.name}}, ns={{namespace}}",
		tnt,
		ns,
	)

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
	tenant.TemplateForTenantAndNamespace(m, tnt, ns)

	if got := m["key1"]; got != "tenant=tenant-a, ns=ns-1" {
		t.Fatalf("key1: expected %q, got %q", "tenant=tenant-a, ns=ns-1", got)
	}

	if got := m["key2"]; got != "plain-value" {
		t.Fatalf("key2: expected %q to remain unchanged, got %q", "plain-value", got)
=======
	want := "tenant=tenant-a, ns=ns-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go
	}
}

func TestTemplateForTenantAndNamespace_ReplacesPlaceholdersSpaces(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	got := tpl.TemplateForTenantAndNamespace(
		"tenant={{ tenant.name }}, ns={{ namespace }}",
		tnt,
		ns,
	)

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
	original1 := m["noTemplate1"]
	original2 := m["noTemplate2"]

	tenant.TemplateForTenantAndNamespace(m, tnt, ns)

	if got := m["noTemplate1"]; got != original1 {
		t.Fatalf("noTemplate1: expected %q to remain unchanged, got %q", original1, got)
	}
	if got := m["noTemplate2"]; got != original2 {
		t.Fatalf("noTemplate2: expected %q to remain unchanged, got %q", original2, got)
=======
	want := "tenant=tenant-a, ns=ns-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go
	}
}

func TestTemplateForTenantAndNamespace_OnlyTenant(t *testing.T) {
	tnt := newTenant("tenant-x")
	ns := newNamespace("ns-y")

	got := tpl.TemplateForTenantAndNamespace("T={{tenant.name}}", tnt, ns)
	want := "T=tenant-x"

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
	tenant.TemplateForTenantAndNamespace(m, tnt, ns)
=======
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go

func TestTemplateForTenantAndNamespace_OnlyNamespace(t *testing.T) {
	tnt := newTenant("tenant-x")
	ns := newNamespace("ns-y")

	got := tpl.TemplateForTenantAndNamespace("N={{namespace}}", tnt, ns)
	want := "N=ns-y"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespace_NoDelimiters_ReturnsInput(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	in := "plain-value-without-templates"
	got := tpl.TemplateForTenantAndNamespace(in, tnt, ns)

	if got != in {
		t.Fatalf("expected %q, got %q", in, got)
	}
}

func TestTemplateForTenantAndNamespace_UnknownKeyBecomesEmpty(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	got := tpl.TemplateForTenantAndNamespace("X={{unknown.key}}", tnt, ns)
	want := "X="

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateForTenantAndNamespaceMap_ReplacesPlaceholders(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	orig := map[string]string{
		"key1": "tenant={{tenant.name}}, ns={{namespace}}",
		"key2": "plain-value",
	}

	out := tpl.TemplateForTenantAndNamespaceMap(orig, tnt, ns)

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
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	orig := map[string]string{
		"key1": "tenant={{ tenant.name }}, ns={{ namespace }}",
		"key2": "plain-value",
	}

	out := tpl.TemplateForTenantAndNamespaceMap(orig, tnt, ns)

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
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	orig := map[string]string{
		"t1": "hello {{tenant.name}}",
		"t2": "namespace {{namespace}}",
		"t3": "static",
	}

	out := tpl.TemplateForTenantAndNamespaceMap(orig, tnt, ns)

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
	tnt := newTenant("tenant-x")
	ns := newNamespace("ns-x")

	orig := map[string]string{
		"onlyTenant": "T={{ tenant.name }}",
		"onlyNS":     "N={{ namespace }}",
		"none":       "static",
	}

	out := tpl.TemplateForTenantAndNamespaceMap(orig, tnt, ns)

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
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	orig := map[string]string{
		"unknown": "X={{ unknown.key }}",
	}

<<<<<<< HEAD:pkg/utils/tenant/fast_template_test.go
	tenant.TemplateForTenantAndNamespace(m, tnt, ns)
=======
	out := tpl.TemplateForTenantAndNamespaceMap(orig, tnt, ns)
>>>>>>> 7efaa9eb460450f9c60905f0eacf4bfe42a9d470:pkg/template/fast_test.go

	if got := out["unknown"]; got != "X=" {
		t.Fatalf("unknown: expected %q, got %q", "X=", got)
	}
}

func TestTemplateForTenantAndNamespaceMap_EmptyOrNilInput(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

	// nil map
	outNil := tpl.TemplateForTenantAndNamespaceMap(nil, tnt, ns)
	if outNil == nil {
		t.Fatalf("expected non-nil map for nil input")
	}
	if len(outNil) != 0 {
		t.Fatalf("expected empty map for nil input, got %v", outNil)
	}

	// empty map
	outEmpty := tpl.TemplateForTenantAndNamespaceMap(map[string]string{}, tnt, ns)
	if outEmpty == nil || len(outEmpty) != 0 {
		t.Fatalf("expected empty map, got %v", outEmpty)
	}
}

// Concurrency test: should never panic with "concurrent map writes"
// Run with: go test -race ./...
func TestTemplateForTenantAndNamespaceMap_Concurrency(t *testing.T) {
	tnt := newTenant("tenant-a")
	ns := newNamespace("ns-1")

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
				out := tpl.TemplateForTenantAndNamespaceMap(shared, tnt, ns)

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
