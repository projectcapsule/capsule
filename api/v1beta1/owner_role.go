// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"fmt"
	"strings"
)

const (
	ClusterRoleNamesAnnotation = "clusterrolenames.capsule.clastix.io"
)

func (in OwnerSpec) GetRoles(tenant Tenant) []string {
	for key, value := range tenant.GetAnnotations() {
		if key == fmt.Sprintf("%s/%s.%s", ClusterRoleNamesAnnotation, strings.ToLower(in.Kind.String()), strings.ToLower(in.Name)) {
			return strings.Split(value, ",")
		}
	}

	return []string{"admin", "capsule-namespace-deleter"}
}
