// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

func GetOwnersWithKinds(tenant *capsulev1beta2.Tenant) (owners []string) {
	for _, owner := range tenant.Spec.Owners {
		owners = append(owners, fmt.Sprintf("%s:%s", owner.Kind.String(), owner.Name))
	}

	return
}
