// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func GetOwnersWithKinds(tenant *capsulev1beta2.Tenant) (owners []string) {
	for _, owner := range tenant.Status.Owners {
		owners = append(owners, fmt.Sprintf("%s:%s", owner.Kind.String(), owner.Name))
	}

	return owners
}
