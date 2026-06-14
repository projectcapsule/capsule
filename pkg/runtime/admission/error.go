// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func ErroredResponse(err error) *admission.Response {
	return new(admission.Errored(http.StatusInternalServerError, err))
}
