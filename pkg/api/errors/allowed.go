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
		extra = append(extra, fmt.Sprintf("use one from the following list (%s)", strings.Join(allowed.Exact, ", ")))
	}

	//nolint:staticcheck
	if len(allowed.Regex) > 0 {
		extra = append(extra, fmt.Sprintf("use one matching the following regex (%s)", allowed.Regex))
	}

	if len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 {
		extra = append(extra, "matching the label selector defined in the Tenant")
	}

	err += strings.Join(extra, " or ")

	return err
}

func SelectionListWithDefaultErrorMessage(allowed api.SelectionListWithDefaultSpec, err string) string {
	var extra []string
	if len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 {
		extra = append(extra, "matching the label selector defined in the Tenant")
	}

	err += strings.Join(extra, " or ")

	return err
}
