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

func SplitFieldSelectorEquals(raw string) (path string, value string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	idx, width := findTopLevelEquals(raw)
	if idx < 0 {
		return "", "", false
	}

	path = strings.TrimSpace(raw[:idx])
	value = strings.TrimSpace(raw[idx+width:])

	if path == "" || value == "" {
		return "", "", false
	}

	value = trimMatchingQuotes(value)

	return path, value, true
}

func findTopLevelEquals(raw string) (idx int, width int) {
	var (
		bracketDepth int
		braceDepth   int
		parenDepth   int
		quote        byte
		escaped      bool
	)

	for i := range len(raw) {
		ch := raw[i]

		if escaped {
			escaped = false

			continue
		}

		if quote != 0 {
			switch ch {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}

			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch

		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}

		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}

		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}

		case '=':
			if bracketDepth != 0 || braceDepth != 0 || parenDepth != 0 {
				continue
			}

			if i+1 < len(raw) && raw[i+1] == '=' {
				return i, 2
			}

			return i, 1
		}
	}

	return -1, 0
}

func trimMatchingQuotes(value string) string {
	if len(value) < 2 {
		return value
	}

	first := value[0]
	last := value[len(value)-1]

	if first != last {
		return value
	}

	if first != '"' && first != '\'' {
		return value
	}

	return strings.TrimSpace(value[1 : len(value)-1])
}
