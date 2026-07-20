// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota_test

import (
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCustomQuotaCacheKeys(t *testing.T) {
	t.Parallel()

	if got := quota.MakeCustomQuotaCacheKey("tenant-a", "quota-a"); got != "tenant-a/quota-a" {
		t.Fatalf("MakeCustomQuotaCacheKey() = %q", got)
	}
	if got := quota.MakeGlobalCustomQuotaCacheKey("quota-a"); got != "C/quota-a" {
		t.Fatalf("MakeGlobalCustomQuotaCacheKey() = %q", got)
	}
}

func TestParseBoolFromUnstructured(t *testing.T) {
	t.Parallel()

	compiled, err := jsonpath.CompileJSONPath(".spec.enabled")
	if err != nil {
		t.Fatalf("CompileJSONPath() unexpected error: %v", err)
	}
	empty, err := jsonpath.CompileJSONPath(".spec.empty")
	if err != nil {
		t.Fatalf("CompileJSONPath() unexpected error: %v", err)
	}

	u := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"enabled": "true",
			"empty":   "",
		},
	}}

	got, err := quota.ParseBoolFromUnstructured(u, compiled)
	if err != nil {
		t.Fatalf("ParseBoolFromUnstructured() unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("ParseBoolFromUnstructured() = false, want true")
	}

	got, err = quota.ParseBoolFromUnstructured(u, empty)
	if err != nil {
		t.Fatalf("ParseBoolFromUnstructured(empty) unexpected error: %v", err)
	}
	if got {
		t.Fatalf("ParseBoolFromUnstructured(empty) = true, want false")
	}

	u.Object["spec"].(map[string]any)["enabled"] = "not-bool"
	if _, err := quota.ParseBoolFromUnstructured(u, compiled); err == nil {
		t.Fatalf("ParseBoolFromUnstructured(invalid) expected error")
	}
}

func TestConditionsMatch(t *testing.T) {
	t.Parallel()

	enabled, err := jsonpath.CompileJSONPath(".spec.enabled")
	if err != nil {
		t.Fatalf("CompileJSONPath() unexpected error: %v", err)
	}
	ready, err := jsonpath.CompileJSONPath(".spec.ready")
	if err != nil {
		t.Fatalf("CompileJSONPath() unexpected error: %v", err)
	}

	u := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"enabled": "true",
			"ready":   "false",
		},
	}}

	got, err := quota.ConditionsMatch(u, []*jsonpath.CompiledJSONPath{enabled})
	if err != nil {
		t.Fatalf("ConditionsMatch() unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("ConditionsMatch() = false, want true")
	}

	got, err = quota.ConditionsMatch(u, []*jsonpath.CompiledJSONPath{enabled, ready})
	if err != nil {
		t.Fatalf("ConditionsMatch() unexpected error: %v", err)
	}
	if got {
		t.Fatalf("ConditionsMatch() = true, want false")
	}
}
