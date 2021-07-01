// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"

func GetOwnerWithKind(tenant *capsulev1beta1.Tenant) string {
	return tenant.Spec.Owner.Kind.String() + ":" + tenant.Spec.Owner.Name
}
