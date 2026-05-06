// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
func TestParseQuantities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		want        resource.Quantity
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty string returns error",
			input:       "",
			wantErr:     true,
			errContains: "no quantity values found",
		},
		{
			name:        "whitespace only returns error",
			input:       "   \n\t   ",
			wantErr:     true,
			errContains: "no quantity values found",
		},
		{
			name:    "single quantity",
			input:   "100m",
			want:    resource.MustParse("100m"),
			wantErr: false,
		},
		{
			name:    "multiple cpu quantities",
			input:   "100m 200m 300m",
			want:    resource.MustParse("600m"),
			wantErr: false,
		},
		{
			name:    "multiple memory quantities",
			input:   "128Mi 256Mi 1Gi",
			want:    resource.MustParse("1408Mi"),
			wantErr: false,
		},
		{
			name:    "mixed whitespace separators",
			input:   "100m\t200m\n300m",
			want:    resource.MustParse("600m"),
			wantErr: false,
		},
		{
			name:    "equivalent units",
			input:   "1Gi 512Mi 512Mi",
			want:    resource.MustParse("2Gi"),
			wantErr: false,
		},
		{
			name:        "invalid quantity returns error",
			input:       "100m invalid 200m",
			wantErr:     true,
			errContains: `invalid quantity "invalid"`,
		},
		{
			name:        "completely invalid input returns error",
			input:       "nope",
			wantErr:     true,
			errContains: `invalid quantity "nope"`,
		},
		{
			name:    "decimal SI quantities",
			input:   "1 2 3",
			want:    resource.MustParse("6"),
			wantErr: false,
		},
		{
			name:    "binary SI quantities",
			input:   "1Ki 2Ki 3Ki",
			want:    resource.MustParse("6Ki"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := quota.ParseQuantities(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got.Cmp(tt.want) != 0 {
				t.Fatalf("expected quantity %q, got %q", tt.want.String(), got.String())
			}
		})
	}
}

func TestParseQuantities_ReturnsZeroOnFirstInvalidValue(t *testing.T) {
	t.Parallel()

	got, err := quota.ParseQuantities("100m bad 200m")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	zero := resource.Quantity{}
	if got.Cmp(zero) != 0 {
		t.Fatalf("expected returned quantity to be zero on error, got %q", got.String())
	}
}

func TestParseQuantityFromUnstructured_Success(t *testing.T) {
	t.Parallel()

	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu": "250m",
					},
				},
			},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.resources.requests.cpu")
	if err != nil {
		t.Fatalf("expected no error compiling jsonpath, got %v", err)
	}

	got, err := quota.ParseQuantityFromUnstructured(u, jp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	want := resource.MustParse("250m")
	if got.Cmp(want) != 0 {
		t.Fatalf("expected quantity %q, got %q", want.String(), got.String())
	}
}

func TestParseQuantityFromUnstructured_MissingPathReturnsError(t *testing.T) {
	t.Parallel()

	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.resources.requests.cpu")
	if err != nil {
		t.Fatalf("expected no error compiling jsonpath, got %v", err)
	}

	_, err = quota.ParseQuantityFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !contains(err.Error(), "did not resolve to any value") &&
		!contains(err.Error(), "no quantity values found") {
		t.Fatalf("expected missing quantity error, got %q", err.Error())
	}
}

func TestParseQuantityFromUnstructured_WhitespaceOnlyReturnsError(t *testing.T) {
	t.Parallel()

	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"value": "   \n\t   ",
			},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.value")
	if err != nil {
		t.Fatalf("expected no error compiling jsonpath, got %v", err)
	}

	_, err = quota.ParseQuantityFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !contains(err.Error(), "did not resolve to any value") &&
		!contains(err.Error(), "no quantity values found") {
		t.Fatalf("expected empty quantity error, got %q", err.Error())
	}
}

func TestParseUsageFromUnstructured_Success(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "250m",
						"memory": "512Mi",
					},
				},
			},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.resources.requests.cpu")
	if err == nil {
		t.Fatal("expected no error, got error", err)
	}

	got, err := quota.ParseUsageFromUnstructured(u, jp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != "250m" {
		t.Fatalf("expected %q, got %q", "250m", got)
	}
}

func TestParseUsageFromUnstructured_TrimsWhitespace(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"value": " hello ",
			},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.value")
	if err == nil {
		t.Fatal("expected no error, got error", err)
	}

	got, err := quota.ParseUsageFromUnstructured(u, jp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestParseUsageFromUnstructured_MissingPath(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{},
		},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.resources.requests.cpu")
	if err == nil {
		t.Fatal("expected no error, got error", err)
	}

	_, err = quota.ParseUsageFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseUsageFromUnstructured_InvalidJSONPath(t *testing.T) {
	t.Parallel()

	_, err := jsonpath.CompileJSONPath(".spec[")
	if err == nil {
		t.Fatal("expected compile error, got nil")
	}
}

func TestParseUsageFromUnstructured_EmptySourcePath(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{},
	}

	jp, err := jsonpath.CompileJSONPath("")
	if err != nil {
		t.Fatal("expected error, got nil")
	}

	_, err = quota.ParseUsageFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseUsageFromUnstructured_SourcePathMustStartWithDot(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{},
	}

	jp, err := jsonpath.CompileJSONPath("spec.resources.requests.cpu")
	if err != nil {
		t.Fatal("expected error, got nil")
	}

	_, err = quota.ParseUsageFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseUsageFromUnstructured_RejectsControlWhitespace(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]interface{}{},
	}

	jp, err := jsonpath.CompileJSONPath(".spec.\nrequests.cpu")
	if err != nil {
		t.Fatal("expected error, got nil")
	}

	_, err = quota.ParseUsageFromUnstructured(u, jp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
