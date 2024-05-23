// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const (
	ObjectReferenceTenantKind = "Tenant"
)

func IsTenantOwnerReference(or metav1.OwnerReference) bool {
	parts := strings.Split(or.APIVersion, "/")
	if len(parts) != 2 {
		return false
	}

	group := parts[0]

	return group == capsulev1beta2.GroupVersion.Group && or.Kind == ObjectReferenceTenantKind
}
