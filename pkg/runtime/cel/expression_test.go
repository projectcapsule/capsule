// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cel

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apiserver/pkg/cel/environment"
)

func TestCompiledExpressionEvaluateBoolean(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileBoolean(
		`object.spec.containers.exists(c, c.image == "nginx:1.27.0")`,
		environment.StoredExpressions,
	)
	if err != nil {
		t.Fatalf("CompileBoolean() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "main", "image": "nginx:1.27.0"},
			},
		},
	}}

	matched, err := compiled.EvaluateBoolean(context.Background(), object)
	if err != nil {
		t.Fatalf("EvaluateBoolean() error = %v", err)
	}
	if !matched {
		t.Fatal("EvaluateBoolean() = false, want true")
	}
}

func TestCompileBooleanRejectsNonBooleanResult(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	_, err = compiler.CompileBoolean(`object.metadata.name`, environment.StoredExpressions)
	if err == nil || !strings.Contains(err.Error(), "must evaluate to bool") {
		t.Fatalf("CompileBoolean() error = %v, want boolean result type error", err)
	}
}

func TestCompileBooleanWithVariables(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileBooleanWithVariables(
		`requestor.name == "alice" && "admin" in reviewer.groups`,
		environment.StoredExpressions,
		"requestor",
		"reviewer",
	)
	if err != nil {
		t.Fatalf("CompileBooleanWithVariables() error = %v", err)
	}

	got, err := compiled.EvaluateBooleanWithVariables(context.Background(), map[string]any{
		"requestor": map[string]any{"name": "alice"},
		"reviewer":  map[string]any{"groups": []string{"users", "admin"}},
	})
	if err != nil {
		t.Fatalf("EvaluateBooleanWithVariables() error = %v", err)
	}
	if !got {
		t.Fatal("EvaluateBooleanWithVariables() = false, want true")
	}
}

func TestCompileStringWithVariables(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileStringWithVariables(
		`"invalid subject " + self.subjectName`,
		environment.NewExpressions,
		"self",
	)
	if err != nil {
		t.Fatalf("CompileStringWithVariables() error = %v", err)
	}

	got, err := compiled.EvaluateStringWithVariables(context.Background(), map[string]any{
		"self": map[string]any{"subjectName": "runner"},
	})
	if err != nil {
		t.Fatalf("EvaluateStringWithVariables() error = %v", err)
	}

	if got != "invalid subject runner" {
		t.Fatalf("EvaluateStringWithVariables() = %q, want %q", got, "invalid subject runner")
	}
}

func TestCompiledExpressionEvaluateSingleQuantity(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileQuantity(
		`quantity(object.spec.resources.requests["storage"])`,
		environment.StoredExpressions,
	)
	if err != nil {
		t.Fatalf("CompileQuantity() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"resources": map[string]any{
				"requests": map[string]any{"storage": "2Gi"},
			},
		},
	}}

	got, err := compiled.EvaluateQuantity(context.Background(), object)
	if err != nil {
		t.Fatalf("EvaluateQuantity() error = %v", err)
	}
	if got.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("EvaluateQuantity() = %s, want 2Gi", got.String())
	}
}

func TestCompiledExpressionSumsQuantityList(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileQuantity(
		`object.spec.containers`+
			`.filter(c, has(c.resources) && has(c.resources.requests) && "cpu" in c.resources.requests)`+
			`.map(c, quantity(c.resources.requests["cpu"]))`,
		environment.StoredExpressions,
	)
	if err != nil {
		t.Fatalf("CompileQuantity() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "250m"},
					},
				},
				map[string]any{
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "500m"},
					},
				},
				map[string]any{"name": "no-request"},
			},
		},
	}}

	got, err := compiled.EvaluateQuantity(context.Background(), object)
	if err != nil {
		t.Fatalf("EvaluateQuantity() error = %v", err)
	}
	if got.Cmp(resource.MustParse("750m")) != 0 {
		t.Fatalf("EvaluateQuantity() = %s, want 750m", got.String())
	}
}

func TestCompiledExpressionRejectsEmptyQuantityList(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	compiled, err := compiler.CompileQuantity(
		`object.spec.values.map(v, quantity(v))`,
		environment.StoredExpressions,
	)
	if err != nil {
		t.Fatalf("CompileQuantity() error = %v", err)
	}

	object := unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"values": []any{}},
	}}

	_, err = compiled.EvaluateQuantity(context.Background(), object)
	if err == nil || !strings.Contains(err.Error(), "empty quantity list") {
		t.Fatalf("EvaluateQuantity() error = %v, want empty list error", err)
	}
}

func TestCompileQuantityRejectsStringResult(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	_, err = compiler.CompileQuantity(
		`string(object.spec.value)`,
		environment.StoredExpressions,
	)
	if err == nil || !strings.Contains(err.Error(), "kubernetes.Quantity") {
		t.Fatalf("CompileQuantity() error = %v, want quantity result type error", err)
	}
}
