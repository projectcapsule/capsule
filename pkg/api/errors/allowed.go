// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
)

func DefaultAllowedValuesErrorMessage(allowed api.DefaultAllowedListSpec, err string) string {
	return AllowedValuesErrorMessage(allowed.SelectorAllowedListSpec, err)
}

func AllowedValuesErrorMessage(allowed api.SelectorAllowedListSpec, err string) string {
	var extra []string
	if len(allowed.Exact) > 0 {
		extra = append(extra, strings.Join(allowed.Exact, ", "))
	}

	//nolint:staticcheck
	if len(allowed.Regex) > 0 {
		extra = append(extra, "matching pattern "+allowed.Regex)
	}

	if len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 {
		extra = append(extra, "matching the tenant's label selector")
	}

	return appendAllowedValues(err, extra)
}

func SelectionListWithDefaultErrorMessage(allowed api.SelectionListWithDefaultSpec, err string) string {
	var extra []string
	if len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 {
		extra = append(extra, "matching the tenant's label selector")
	}

	return appendAllowedValues(err, extra)
}

func appendAllowedValues(message string, choices []string) string {
	message = strings.TrimRight(strings.TrimSpace(message), ":.")
	if len(choices) == 0 {
		return message
	}

	if message == "" {
		return "Allowed values: " + strings.Join(choices, " or ")
	}

	return fmt.Sprintf("%s. Allowed values: %s", message, strings.Join(choices, " or "))
}
