// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"fmt"
	"strings"
)

func wrapJSONPath(sourcePath string) string {
	return fmt.Sprintf("{%s}", sourcePath)
}

func validateSourcePath(sourcePath string) error {
	if sourcePath == "" {
		return fmt.Errorf("sourcePath must not be empty")
	}

	if len(sourcePath) > maxJSONPathLength {
		return fmt.Errorf("sourcePath exceeds max length of %d", maxJSONPathLength)
	}

	if !strings.HasPrefix(sourcePath, ".") {
		return fmt.Errorf("sourcePath must start with '.'")
	}

	if strings.ContainsAny(sourcePath, "\r\n\t") {
		return fmt.Errorf("sourcePath must not contain control whitespace")
	}

	// Braces would be swallowed as jsonpath template delimiters when the
	// path is wrapped, letting ".spec.foo}{.spec.bar" smuggle in a second
	// expression.
	if strings.ContainsAny(sourcePath, "{}") {
		return fmt.Errorf("sourcePath must not contain curly braces")
	}

	return nil
}
