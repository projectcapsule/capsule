// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package admission

import (
	"path"
	"strings"
)

func normalizePath(p string) string {
	if p == "" {
		return ""
	}

	p = "/" + strings.TrimLeft(p, "/")

	return path.Clean(p)
}
