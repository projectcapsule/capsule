// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEvaluateTruthyFromCompiled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		object  unstructured.Unstructured
		want    bool
		wantErr bool
	}{
		{
			name: "empty missing path is false",
			path: ".missing",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"type": "ClusterIP",
				},
			}),
			want: false,
		},
		{
			name: "empty string is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "",
				},
			}),
			want: false,
		},
		{
			name: "whitespace string is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "   ",
				},
			}),
			want: false,
		},
		{
			name: "false string is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "false",
				},
			}),
			want: false,
		},
		{
			name: "false string case insensitive is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "FALSE",
				},
			}),
			want: false,
		},
		{
			name: "false string with whitespace is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": " false ",
				},
			}),
			want: false,
		},
		{
			name: "zero string is false",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "0",
				},
			}),
			want: false,
		},
		{
			name: "true string is true",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "true",
				},
			}),
			want: true,
		},
		{
			name: "non-empty scalar string is true",
			path: ".spec.type",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"type": "ClusterIP",
				},
			}),
			want: true,
		},
		{
			name: "one string is true",
			path: ".spec.value",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"value": "1",
				},
			}),
			want: true,
		},
		{
			name: "jsonpath filter match is true",
			path: ".spec.accessModes[?(@==\"ReadWriteOnce\")]",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"accessModes": []any{
						"ReadWriteOnce",
						"ReadOnlyMany",
					},
				},
			}),
			want: true,
		},
		{
			name: "jsonpath filter no match is false",
			path: ".spec.accessModes[?(@==\"ReadWriteMany\")]",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"accessModes": []any{
						"ReadWriteOnce",
						"ReadOnlyMany",
					},
				},
			}),
			want: false,
		},
		{
			name: "invalid jsonpath execution returns error",
			path: ".spec.accessModes[999]",
			object: newUnstructured(map[string]any{
				"spec": map[string]any{
					"accessModes": []any{
						"ReadWriteOnce",
					},
				},
			}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			compiled, err := CompileJSONPath(tt.path)
			if err != nil {
				t.Fatalf("expected jsonpath %q to compile, got %v", tt.path, err)
			}

			got, err := EvaluateTruthyFromCompiled(tt.object, compiled)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestEvaluateTruthyFromCompiledRejectsNilCompiledJSONPath(t *testing.T) {
	t.Parallel()

	got, err := EvaluateTruthyFromCompiled(newUnstructured(nil), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got {
		t.Fatal("expected false result for nil compiled jsonpath")
	}

	if !strings.Contains(err.Error(), "compiled jsonpath is nil") {
		t.Fatalf("expected nil compiled jsonpath error, got %v", err)
	}
}

func TestSplitFieldSelectorEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantPath  string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "single equals",
			raw:       `.spec.type=ClusterIP`,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP`,
			wantOK:    true,
		},
		{
			name:      "double equals",
			raw:       `.spec.type==ClusterIP`,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP`,
			wantOK:    true,
		},
		{
			name:      "double equals with quoted value",
			raw:       `.spec.type=="ClusterIP"`,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP`,
			wantOK:    true,
		},
		{
			name:      "double equals with single quoted value",
			raw:       `.spec.type=='ClusterIP'`,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP`,
			wantOK:    true,
		},
		{
			name:      "trims whitespace around expression",
			raw:       `  .spec.type == "ClusterIP"  `,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP`,
			wantOK:    true,
		},
		{
			name:      "keeps spaces inside quoted value",
			raw:       `.metadata.annotations["example.com/value"]=="hello world"`,
			wantPath:  `.metadata.annotations["example.com/value"]`,
			wantValue: `hello world`,
			wantOK:    true,
		},
		{
			name:      "quoted value containing equals",
			raw:       `.metadata.annotations["example.com/check"]=="a=b"`,
			wantPath:  `.metadata.annotations["example.com/check"]`,
			wantValue: `a=b`,
			wantOK:    true,
		},
		{
			name:      "single quoted value containing equals",
			raw:       `.metadata.annotations["example.com/check"]=='a=b'`,
			wantPath:  `.metadata.annotations["example.com/check"]`,
			wantValue: `a=b`,
			wantOK:    true,
		},
		{
			name:      "top level equality after bracket expression",
			raw:       `.metadata.labels["app.kubernetes.io/name"]=="nginx"`,
			wantPath:  `.metadata.labels["app.kubernetes.io/name"]`,
			wantValue: `nginx`,
			wantOK:    true,
		},
		{
			name:   "jsonpath filter equality is not top level equality",
			raw:    `.spec.accessModes[?(@=="ReadWriteOnce")]`,
			wantOK: false,
		},
		{
			name:   "jsonpath filter single equals is not top level equality",
			raw:    `.spec.accessModes[?(@="ReadWriteOnce")]`,
			wantOK: false,
		},
		{
			name:   "truthy path without equals",
			raw:    `.spec.storageClassName`,
			wantOK: false,
		},
		{
			name:   "empty string",
			raw:    ``,
			wantOK: false,
		},
		{
			name:   "whitespace string",
			raw:    `   `,
			wantOK: false,
		},
		{
			name:   "missing path",
			raw:    `=ClusterIP`,
			wantOK: false,
		},
		{
			name:   "missing value",
			raw:    `.spec.type=`,
			wantOK: false,
		},
		{
			name:   "missing value with double equals",
			raw:    `.spec.type==`,
			wantOK: false,
		},
		{
			name:      "does not split equals inside double quoted path segment",
			raw:       `.metadata.annotations["example.com/a=b"]=="value"`,
			wantPath:  `.metadata.annotations["example.com/a=b"]`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split equals inside single quoted path segment",
			raw:       `.metadata.annotations['example.com/a=b']=="value"`,
			wantPath:  `.metadata.annotations['example.com/a=b']`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split equals inside parentheses",
			raw:       `.spec.values(@=="ignored")=="value"`,
			wantPath:  `.spec.values(@=="ignored")`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split equals inside braces",
			raw:       `.spec.values{"a==b"}=="value"`,
			wantPath:  `.spec.values{"a==b"}`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "escaped quote inside quoted value",
			raw:       `.metadata.annotations["example.com/check"]=="a\"=b"`,
			wantPath:  `.metadata.annotations["example.com/check"]`,
			wantValue: `a\"=b`,
			wantOK:    true,
		},
		{
			name:      "unmatched quote after equality remains part of value",
			raw:       `.spec.type=="ClusterIP`,
			wantPath:  `.spec.type`,
			wantValue: `"ClusterIP`,
			wantOK:    true,
		},
		{
			name:   "unmatched quote before equality suppresses split",
			raw:    `.metadata.annotations["broken]==value`,
			wantOK: false,
		},
		{
			name:   "unmatched bracket suppresses split",
			raw:    `.metadata.annotations["key"=="value"`,
			wantOK: false,
		},
		{
			name:      "top level equality before later malformed content still splits",
			raw:       `.spec.type=ClusterIP[?(@=="ignored")]`,
			wantPath:  `.spec.type`,
			wantValue: `ClusterIP[?(@=="ignored")]`,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPath, gotValue, gotOK := SplitFieldSelectorEquals(tt.raw)
			if gotOK != tt.wantOK {
				t.Fatalf("expected ok=%t, got %t", tt.wantOK, gotOK)
			}

			if gotPath != tt.wantPath {
				t.Fatalf("expected path %q, got %q", tt.wantPath, gotPath)
			}

			if gotValue != tt.wantValue {
				t.Fatalf("expected value %q, got %q", tt.wantValue, gotValue)
			}
		})
	}
}

func TestFindTopLevelEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantIdx   int
		wantWidth int
	}{
		{
			name:      "single equals",
			raw:       `.spec.type=ClusterIP`,
			wantIdx:   len(`.spec.type`),
			wantWidth: 1,
		},
		{
			name:      "double equals",
			raw:       `.spec.type==ClusterIP`,
			wantIdx:   len(`.spec.type`),
			wantWidth: 2,
		},
		{
			name:      "ignores equals inside brackets",
			raw:       `.spec.accessModes[?(@=="ReadWriteOnce")]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "finds top level equals after brackets",
			raw:       `.metadata.labels["app.kubernetes.io/name"]=="nginx"`,
			wantIdx:   len(`.metadata.labels["app.kubernetes.io/name"]`),
			wantWidth: 2,
		},
		{
			name:      "ignores equals inside double quotes",
			raw:       `.metadata.annotations["a=b"]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores equals inside single quotes",
			raw:       `.metadata.annotations['a=b']`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores escaped quote before equals inside quoted string",
			raw:       `.metadata.annotations["a\"=b"]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores equals inside parentheses",
			raw:       `.spec.values(@=="ignored")`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores equals inside braces",
			raw:       `.spec.values{"a==b"}`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "finds first top level equals",
			raw:       `.spec.type=ClusterIP=ignored`,
			wantIdx:   len(`.spec.type`),
			wantWidth: 1,
		},
		{
			name:      "empty string",
			raw:       ``,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "no equals",
			raw:       `.spec.type`,
			wantIdx:   -1,
			wantWidth: 0,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIdx, gotWidth := findTopLevelEquals(tt.raw)
			if gotIdx != tt.wantIdx {
				t.Fatalf("expected idx %d, got %d", tt.wantIdx, gotIdx)
			}

			if gotWidth != tt.wantWidth {
				t.Fatalf("expected width %d, got %d", tt.wantWidth, gotWidth)
			}
		})
	}
}

func TestTrimMatchingQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "empty",
			value: "",
			want:  "",
		},
		{
			name:  "single character",
			value: `"`,
			want:  `"`,
		},
		{
			name:  "double quoted",
			value: `"ClusterIP"`,
			want:  "ClusterIP",
		},
		{
			name:  "single quoted",
			value: `'ClusterIP'`,
			want:  "ClusterIP",
		},
		{
			name:  "trims inner whitespace",
			value: `" ClusterIP "`,
			want:  "ClusterIP",
		},
		{
			name:  "unmatched starting quote",
			value: `"ClusterIP`,
			want:  `"ClusterIP`,
		},
		{
			name:  "unmatched ending quote",
			value: `ClusterIP"`,
			want:  `ClusterIP"`,
		},
		{
			name:  "different quote types",
			value: `"ClusterIP'`,
			want:  `"ClusterIP'`,
		},
		{
			name:  "unquoted",
			value: `ClusterIP`,
			want:  `ClusterIP`,
		},
		{
			name:  "quoted value containing equals",
			value: `"a=b"`,
			want:  `a=b`,
		},
		{
			name:  "quoted value containing escaped quote",
			value: `"a\"=b"`,
			want:  `a\"=b`,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := trimMatchingQuotes(tt.value); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestSplitFieldSelectorNotEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantPath  string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "simple not-equals",
			raw:       `.status.phase!=Succeeded`,
			wantPath:  `.status.phase`,
			wantValue: `Succeeded`,
			wantOK:    true,
		},
		{
			name:      "not-equals with double-quoted value",
			raw:       `.status.phase!="Succeeded"`,
			wantPath:  `.status.phase`,
			wantValue: `Succeeded`,
			wantOK:    true,
		},
		{
			name:      "not-equals with single-quoted value",
			raw:       `.status.phase!='Succeeded'`,
			wantPath:  `.status.phase`,
			wantValue: `Succeeded`,
			wantOK:    true,
		},
		{
			name:      "trims whitespace around expression",
			raw:       `  .status.phase != "Succeeded"  `,
			wantPath:  `.status.phase`,
			wantValue: `Succeeded`,
			wantOK:    true,
		},
		{
			name:      "keeps spaces inside quoted value",
			raw:       `.metadata.annotations["example.com/value"]!="hello world"`,
			wantPath:  `.metadata.annotations["example.com/value"]`,
			wantValue: `hello world`,
			wantOK:    true,
		},
		{
			name:      "value containing not-equals in quotes",
			raw:       `.metadata.annotations["example.com/check"]!="a!=b"`,
			wantPath:  `.metadata.annotations["example.com/check"]`,
			wantValue: `a!=b`,
			wantOK:    true,
		},
		{
			name:      "top level not-equals after bracket expression",
			raw:       `.metadata.labels["app.kubernetes.io/name"]!="nginx"`,
			wantPath:  `.metadata.labels["app.kubernetes.io/name"]`,
			wantValue: `nginx`,
			wantOK:    true,
		},
		{
			name:   "bang without equals is not not-equals",
			raw:    `.spec.type!ClusterIP`,
			wantOK: false,
		},
		{
			name:   "truthy path without not-equals",
			raw:    `.spec.storageClassName`,
			wantOK: false,
		},
		{
			name:   "plain equals is not not-equals",
			raw:    `.spec.type=ClusterIP`,
			wantOK: false,
		},
		{
			name:   "double equals is not not-equals",
			raw:    `.spec.type==ClusterIP`,
			wantOK: false,
		},
		{
			name:   "empty string",
			raw:    ``,
			wantOK: false,
		},
		{
			name:   "whitespace string",
			raw:    `   `,
			wantOK: false,
		},
		{
			name:   "missing path",
			raw:    `!=Succeeded`,
			wantOK: false,
		},
		{
			name:   "missing value",
			raw:    `.status.phase!=`,
			wantOK: false,
		},
		{
			name:      "does not split not-equals inside double-quoted path segment",
			raw:       `.metadata.annotations["example.com/a!=b"]!="value"`,
			wantPath:  `.metadata.annotations["example.com/a!=b"]`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split not-equals inside single-quoted path segment",
			raw:       `.metadata.annotations['example.com/a!=b']!="value"`,
			wantPath:  `.metadata.annotations['example.com/a!=b']`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split not-equals inside brackets",
			raw:       `.spec.accessModes[?(@!="ReadWriteOnce")]!="value"`,
			wantPath:  `.spec.accessModes[?(@!="ReadWriteOnce")]`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split not-equals inside parentheses",
			raw:       `.spec.values(@!="ignored")!="value"`,
			wantPath:  `.spec.values(@!="ignored")`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:      "does not split not-equals inside braces",
			raw:       `.spec.values{"a!=b"}!="value"`,
			wantPath:  `.spec.values{"a!=b"}`,
			wantValue: `value`,
			wantOK:    true,
		},
		{
			name:   "unmatched bracket suppresses split",
			raw:    `.metadata.annotations["key"!="value"`,
			wantOK: false,
		},
		{
			name:      "unmatched quote after not-equals remains part of value",
			raw:       `.status.phase!="Succeeded`,
			wantPath:  `.status.phase`,
			wantValue: `"Succeeded`,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPath, gotValue, gotOK := SplitFieldSelectorNotEquals(tt.raw)
			if gotOK != tt.wantOK {
				t.Fatalf("expected ok=%t, got %t", tt.wantOK, gotOK)
			}

			if gotPath != tt.wantPath {
				t.Fatalf("expected path %q, got %q", tt.wantPath, gotPath)
			}

			if gotValue != tt.wantValue {
				t.Fatalf("expected value %q, got %q", tt.wantValue, gotValue)
			}
		})
	}
}

func TestFindTopLevelNotEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantIdx   int
		wantWidth int
	}{
		{
			name:      "simple not-equals",
			raw:       `.status.phase!=Succeeded`,
			wantIdx:   len(`.status.phase`),
			wantWidth: 2,
		},
		{
			name:      "ignores not-equals inside brackets",
			raw:       `.spec.accessModes[?(@!="ReadWriteOnce")]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "finds top level not-equals after brackets",
			raw:       `.metadata.labels["app.kubernetes.io/name"]!="nginx"`,
			wantIdx:   len(`.metadata.labels["app.kubernetes.io/name"]`),
			wantWidth: 2,
		},
		{
			name:      "ignores not-equals inside double quotes",
			raw:       `.metadata.annotations["a!=b"]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores not-equals inside single quotes",
			raw:       `.metadata.annotations['a!=b']`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores escaped quote before not-equals inside quoted string",
			raw:       `.metadata.annotations["a\"!=b"]`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores not-equals inside parentheses",
			raw:       `.spec.values(@!="ignored")`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "ignores not-equals inside braces",
			raw:       `.spec.values{"a!=b"}`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "finds first top level not-equals",
			raw:       `.status.phase!=Succeeded!=ignored`,
			wantIdx:   len(`.status.phase`),
			wantWidth: 2,
		},
		{
			name:      "bang without equals is not not-equals",
			raw:       `.spec.type!ClusterIP`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "plain equals is not found",
			raw:       `.spec.type=ClusterIP`,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "empty string",
			raw:       ``,
			wantIdx:   -1,
			wantWidth: 0,
		},
		{
			name:      "no operator",
			raw:       `.spec.type`,
			wantIdx:   -1,
			wantWidth: 0,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIdx, gotWidth := findTopLevelNotEquals(tt.raw)
			if gotIdx != tt.wantIdx {
				t.Fatalf("expected idx %d, got %d", tt.wantIdx, gotIdx)
			}

			if gotWidth != tt.wantWidth {
				t.Fatalf("expected width %d, got %d", tt.wantWidth, gotWidth)
			}
		})
	}
}

func newUnstructured(object map[string]any) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: object,
	}
}
