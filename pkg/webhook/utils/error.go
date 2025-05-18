// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"net/http"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api"
)

func ErroredResponse(err error) *admission.Response {
	response := admission.Errored(http.StatusInternalServerError, err)

	return &response
}

func DefaultAllowedValuesErrorMessage(allowed api.DefaultAllowedListSpec, err string) string {
	var extra []string
	if len(allowed.Exact) > 0 {
		extra = append(extra, fmt.Sprintf("use one from the following list (%s)", strings.Join(allowed.Exact, ", ")))
	}

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
