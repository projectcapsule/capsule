// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"

type sortedTenants []capsulev1beta2.Tenant

func (s sortedTenants) Len() int {
	return len(s)
}

func (s sortedTenants) Less(i, j int) bool {
	return len(s[i].GetName()) < len(s[j].GetName())
}

func (s sortedTenants) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
