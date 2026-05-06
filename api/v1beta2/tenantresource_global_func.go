// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"sort"
)

func (in *GlobalTenantResource) AssignTenants(tnts []Tenant) {
	l := make([]string, 0, len(tnts))

	for _, tnt := range tnts {
		l = append(l, tnt.GetName())
	}

	sort.Strings(l)

	in.Status.SelectedTenants = l
}
