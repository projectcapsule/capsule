// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EvaluateTruthyFromCompiled evaluates a compiled JSONPath expression and interprets the result
// using "truthy" semantics:
//
//   - empty result => false
//   - "false" (case-insensitive) => false
//   - "0" => false
//   - anything else non-empty => true
func EvaluateTruthyFromCompiled(u unstructured.Unstructured, compiled *CompiledJSONPath) (bool, error) {
	if compiled == nil {
		return false, fmt.Errorf("compiled jsonpath is nil")
	}

	value, err := compiled.Execute(u)
	if err != nil {
		return false, err
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}

	switch strings.ToLower(value) {
	case "false", "0":
		return false, nil
	default:
		return true, nil
	}
}
