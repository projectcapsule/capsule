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

// GetRoles read the annotation available in the Tenant specification and if it matches the pattern
// clusterrolenames.capsule.clastix.io/${KIND}.${NAME} returns the associated roles.
// Kubernetes annotations and labels must respect RFC 1123 about DNS names and this could be cumbersome in two cases:
// 1. identifying users based on their email address
// 2. the overall length of the annotation key that is exceeding 63 characters
// For emails, the symbol @ can be replaced with the placeholder __AT__.
// For the latter one, the index of the owner can be used to force the retrieval.
func (in *OwnerSpec) GetRoles(tenant Tenant, index int) []string {
	for key, value := range tenant.GetAnnotations() {
		if !strings.HasPrefix(key, fmt.Sprintf("%s/", ClusterRoleNamesAnnotation)) {
			continue
		}

		for symbol, replace := range in.convertMap() {
			key = strings.ReplaceAll(key, symbol, replace)
		}

		nameBased := key == fmt.Sprintf("%s/%s.%s", ClusterRoleNamesAnnotation, strings.ToLower(in.Kind.String()), strings.ToLower(in.Name))

		indexBased := key == fmt.Sprintf("%s/%d", ClusterRoleNamesAnnotation, index)

		if nameBased || indexBased {
			return strings.Split(value, ",")
		}
	}

	return []string{"admin", "capsule-namespace-deleter"}
}

func (in *OwnerSpec) convertMap() map[string]string {
	return map[string]string{
		"__AT__": "@",
	}
}
