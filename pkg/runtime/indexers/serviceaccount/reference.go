// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package serviceaccount contains keys shared by execution-identity indexes.
package serviceaccount

const ReferenceFieldName = "status.request.impersonation"

func ReferenceKey(namespace, name string) string {
	if namespace == "" || name == "" {
		return ""
	}

	return namespace + "/" + name
}
