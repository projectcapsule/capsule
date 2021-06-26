// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func ErroredResponse(err error) *admission.Response {
	response := admission.Errored(http.StatusInternalServerError, err)

	return &response
}
