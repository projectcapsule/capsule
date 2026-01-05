// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"sort"
)

func (in *GlobalTenantResource) AssignTenants(tnts []Tenant) {
	var l []string

	for _, tnt := range tnts {
		l = append(l, tnt.GetName())
	}

	sort.Strings(l)

	in.Status.SelectedTenants = l
}
