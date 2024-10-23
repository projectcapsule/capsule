// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

// Type to extract all clusterroles for a subject on a tenant
// from the owner and additionalRoleBindings spec.
type TenantSubjectRoles struct {
	Kind         string
	Name         string
	ClusterRoles []string
}
