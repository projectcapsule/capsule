// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import "github.com/clastix/capsule/api/v1alpha1"

func GetOwnerWithKind(tenant *v1alpha1.Tenant) string {
	return tenant.Spec.Owner.Kind.String() + ":" + tenant.Spec.Owner.Name
}
