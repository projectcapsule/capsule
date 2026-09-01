// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import "slices"

// NamespacePresent reports whether the resolved template status includes namespace.
func (s *GlobalBreakRequestTemplateStatus) NamespacePresent(namespace string) bool {
	return slices.Contains(s.Namespaces, "*") || slices.Contains(s.Namespaces, namespace)
}
