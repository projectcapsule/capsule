// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"path"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func Denyf(message string, args ...any) *admission.Response {
	return Deny(fmt.Sprintf(message, args...))
}

func Deny(message string) *admission.Response {
	return new(admission.Denied(message))
}

func Allowf(message string, args ...any) *admission.Response {
	return Allow(fmt.Sprintf(message, args...))
}

func Allow(message string) *admission.Response {
	return new(admission.Allowed(message))
}

func normalizePath(p string) string {
	if p == "" {
		return ""
	}

	p = "/" + strings.TrimLeft(p, "/")

	return path.Clean(p)
}
