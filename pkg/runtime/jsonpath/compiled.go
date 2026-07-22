// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/jsonpath"
)

const maxJSONPathLength = 1024

// CompiledJSONPath wraps a parsed JSONPath expression for repeated use.
type CompiledJSONPath struct {
	jp *jsonpath.JSONPath
}

// CompileJSONPath parses and validates a JSONPath source path once.
// Example sourcePath: ".spec.resources.requests.cpu".
func CompileJSONPath(sourcePath string) (*CompiledJSONPath, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if err := validateSourcePath(sourcePath); err != nil {
		return nil, err
	}

	j := jsonpath.New("usagePath")
	j.AllowMissingKeys(true)

	if err := j.Parse(wrapJSONPath(sourcePath)); err != nil {
		return nil, fmt.Errorf("parse usage jsonpath %q: %w", sourcePath, err)
	}

	return &CompiledJSONPath{jp: j}, nil
}

// Execute applies a precompiled JSONPath to the given object and returns the extracted value.
func (c *CompiledJSONPath) Execute(u unstructured.Unstructured) (string, error) {
	if c == nil || c.jp == nil {
		return "", fmt.Errorf("compiled jsonpath is nil")
	}

	var buf bytes.Buffer
	if err := c.jp.Execute(&buf, u.Object); err != nil {
		return "", fmt.Errorf("execute usage jsonpath: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// FindScalars resolves the compiled path against unstructured object content
// and returns one canonical string per matched scalar, e.g. one value per
// array element for "[*]" expansions.
//
// Paths that fail to resolve, or terminate at maps, arrays, or null, yield
// no value.
func (c *CompiledJSONPath) FindScalars(content map[string]any) []string {
	if c == nil || c.jp == nil || content == nil {
		return nil
	}

	results, err := c.jp.FindResults(content)
	if err != nil {
		return nil
	}

	var out []string

	for _, group := range results {
		for _, result := range group {
			if !result.IsValid() || !result.CanInterface() {
				continue
			}

			value, ok := coerceScalar(result.Interface())
			if !ok {
				continue
			}

			out = append(out, value)
		}
	}

	return out
}

// coerceScalar converts a scalar to its canonical string form. Maps, arrays,
// and null are not scalars and yield no value.
//
// FindScalars only ever runs over unstructured content, whose JSON decoding
// yields exclusively string, bool, int64, float64, map, slice, and nil, so
// only the scalar types below are reachable.
func coerceScalar(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		return "", false
	}
}
