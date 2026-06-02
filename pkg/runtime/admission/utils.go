// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package admission

import (
	"path"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func Deny(message string) *admission.Response {
	response := admission.Denied(message)

	return &response
}

func Allow(message string) *admission.Response {
	response := admission.Allowed(message)

	return &response
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}

	p = "/" + strings.TrimLeft(p, "/")

	return path.Clean(p)
}
