// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCompileJSONPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourcePath string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid simple path",
			sourcePath: ".spec.resources.requests.cpu",
		},
		{
			name:       "valid path with surrounding whitespace",
			sourcePath: "  .spec.resources.requests.memory  ",
		},
		{
			name:       "empty path",
			sourcePath: "",
			wantErr:    true,
			errMsg:     "sourcePath must not be empty",
		},
		{
			name:       "missing leading dot",
			sourcePath: "spec.resources.requests.cpu",
			wantErr:    true,
			errMsg:     "sourcePath must start with '.'",
		},
		{
			name:       "contains newline",
			sourcePath: ".spec.\nresources.requests.cpu",
			wantErr:    true,
			errMsg:     "sourcePath must not contain control whitespace",
		},
		{
			name:       "contains tab",
			sourcePath: ".spec.\tresources.requests.cpu",
			wantErr:    true,
			errMsg:     "sourcePath must not contain control whitespace",
		},
		{
			name:       "too long",
			sourcePath: "." + strings.Repeat("a", maxJSONPathLength),
			wantErr:    true,
			errMsg:     "sourcePath exceeds max length",
		},
		{
			name:       "invalid jsonpath syntax",
			sourcePath: ".spec[",
			wantErr:    true,
			errMsg:     "parse usage jsonpath",
		},
		{
			name:       "wrapped in braces",
			sourcePath: "{.spec.storageClassName}",
			wantErr:    true,
			errMsg:     "sourcePath must start with '.'",
		},
		{
			name:       "template injection via braces",
			sourcePath: ".spec.foo}{.spec.bar",
			wantErr:    true,
			errMsg:     "sourcePath must not contain curly braces",
		},
		{
			name:       "stray brace",
			sourcePath: ".spec.name}",
			wantErr:    true,
			errMsg:     "sourcePath must not contain curly braces",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CompileJSONPath(tt.sourcePath)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
				if got != nil {
					t.Fatalf("expected compiled path to be nil on error, got %#v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected compiled path, got nil")
			}
			if got.jp == nil {
				t.Fatal("expected compiled jsonpath to be initialized, got nil jp")
			}
		})
	}
}

func TestCompiledJSONPathExecute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		compiled  *CompiledJSONPath
		object    unstructured.Unstructured
		want      string
		wantErr   bool
		errMsg    string
		prepareJP string
	}{
		{
			name:     "nil receiver",
			compiled: nil,
			object:   unstructured.Unstructured{},
			wantErr:  true,
			errMsg:   "compiled jsonpath is nil",
		},
		{
			name:     "nil jsonpath",
			compiled: &CompiledJSONPath{},
			object:   unstructured.Unstructured{},
			wantErr:  true,
			errMsg:   "compiled jsonpath is nil",
		},
		{
			name:      "extract string value",
			prepareJP: ".spec.resources.requests.cpu",
			object: unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "250m",
							},
						},
					},
				},
			},
			want: "250m",
		},
		{
			name:      "trim surrounding whitespace",
			prepareJP: ".spec.value",
			object: unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"value": "  hello world  ",
					},
				},
			},
			want: "hello world",
		},
		{
			name:      "extract numeric value",
			prepareJP: ".spec.replicas",
			object: unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"replicas": int64(3),
					},
				},
			},
			want: "3",
		},
		{
			name:      "missing path returns empty string",
			prepareJP: ".spec.resources.requests.memory",
			object: unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "250m",
							},
						},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compiled := tt.compiled
			if tt.prepareJP != "" {
				var err error
				compiled, err = CompileJSONPath(tt.prepareJP)
				if err != nil {
					t.Fatalf("failed to compile jsonpath for test: %v", err)
				}
			}

			got, err := compiled.Execute(tt.object)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%q)", got)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestCompileUsageJSONPath_Execute_Success(t *testing.T) {
	compiled, err := CompileJSONPath(".spec.resources.requests.memory")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"memory": "1Gi",
					},
				},
			},
		},
	}

	got, err := compiled.Execute(u)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != "1Gi" {
		t.Fatalf("expected %q, got %q", "1Gi", got)
	}
}

func TestCompiledJSONPath_Execute_NilReceiver(t *testing.T) {
	var compiled *CompiledJSONPath

	u := unstructured.Unstructured{
		Object: map[string]interface{}{},
	}

	_, err := compiled.Execute(u)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCompiledJSONPathFindScalars(t *testing.T) {
	t.Parallel()

	deployment := map[string]any{
		"spec": map[string]any{
			"replicas": int64(3),
			"template": map[string]any{
				"spec": map[string]any{
					"hostNetwork": true,
					"containers": []any{
						map[string]any{"image": "docker.io/nginx"},
						map[string]any{"image": "ghcr.io/app"},
						map[string]any{"name": "no-image"},
					},
				},
			},
		},
	}

	tests := []struct {
		name       string
		sourcePath string
		content    map[string]any
		want       []string
	}{
		{
			name:       "array expansion over scalar strings",
			sourcePath: ".spec.template.spec.containers[*].image",
			content:    deployment,
			want:       []string{"docker.io/nginx", "ghcr.io/app"},
		},
		{
			name:       "indexed access",
			sourcePath: ".spec.template.spec.containers[1].image",
			content:    deployment,
			want:       []string{"ghcr.io/app"},
		},
		{
			name:       "bool value",
			sourcePath: ".spec.template.spec.hostNetwork",
			content:    deployment,
			want:       []string{"true"},
		},
		{
			name:       "integer value",
			sourcePath: ".spec.replicas",
			content:    deployment,
			want:       []string{"3"},
		},
		{
			name:       "float value",
			sourcePath: ".spec.weight",
			content: map[string]any{
				"spec": map[string]any{
					"weight": 1.5,
				},
			},
			want: []string{"1.5"},
		},
		{
			name:       "scalar array elements",
			sourcePath: ".spec.hosts[*]",
			content: map[string]any{
				"spec": map[string]any{
					"hosts": []any{"a.example.com", "b.example.com"},
				},
			},
			want: []string{"a.example.com", "b.example.com"},
		},
		{
			name:       "missing field",
			sourcePath: ".spec.storageClassName",
			content:    deployment,
			want:       nil,
		},
		{
			name:       "path terminating at a map yields nothing",
			sourcePath: ".spec.template",
			content:    deployment,
			want:       nil,
		},
		{
			name:       "path terminating at an array without expansion yields nothing",
			sourcePath: ".spec.template.spec.containers",
			content:    deployment,
			want:       nil,
		},
		{
			name:       "null value yields nothing",
			sourcePath: ".spec.storageClassName",
			content: map[string]any{
				"spec": map[string]any{
					"storageClassName": nil,
				},
			},
			want: nil,
		},
		{
			name:       "nil content",
			sourcePath: ".spec.name",
			content:    nil,
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compiled, err := CompileJSONPath(tt.sourcePath)
			if err != nil {
				t.Fatalf("expected no compile error, got %v", err)
			}

			got := compiled.FindScalars(tt.content)

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestCompiledJSONPath_FindScalars_NilReceiver(t *testing.T) {
	t.Parallel()

	var compiled *CompiledJSONPath

	if got := compiled.FindScalars(map[string]any{}); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
