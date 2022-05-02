// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

const (
	denyWildcard = "capsule.clastix.io/deny-wildcard"
)

func (t *Tenant) IsWildcardDenied() bool {
	if v, ok := t.Annotations[denyWildcard]; ok && v == "true" {
		return true
	}

	return false
}
