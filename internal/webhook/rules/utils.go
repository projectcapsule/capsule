// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
)

func DescribeExpressionMatch(match api.ExpressionMatch) string {
	parts := make([]string, 0, 3)

	prefix := ""
	if match.Negate {
		prefix = "not "
	}

	if len(match.Exact) > 0 {
		parts = append(parts, fmt.Sprintf("%sexact: %s", prefix, strings.Join(match.Exact, ", ")))
	}

	if match.Expression != "" {
		parts = append(parts, fmt.Sprintf("%sexp: %s", prefix, match.Expression))
	}

	if len(parts) == 0 && match.Negate {
		return "not <empty>"
	}

	return strings.Join(parts, "; ")
}
