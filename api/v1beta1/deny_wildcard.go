// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

const (
	DenyWildcard = "capsule.clastix.io/deny-wildcard"
)

func (in *Tenant) IsWildcardDenied() bool {
	if v, ok := in.Annotations[DenyWildcard]; ok && v == "true" {
		return true
	}

	return false
}
