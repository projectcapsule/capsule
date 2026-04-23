// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"bytes"
	"fmt"
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
// Example sourcePath: ".spec.resources.requests.cpu"
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
