// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const (
	ObjectReferenceTenantKind = "Tenant"
)

func IsTenantOwnerReference(or metav1.OwnerReference) bool {
	if or.Kind != ObjectReferenceTenantKind {
		return false
	}

	if or.APIVersion == "" {
		return false
	}

	parts := strings.Split(or.APIVersion, "/")
	if len(parts) != 2 {
		return false
	}

	group := parts[0]

	return group == capsulev1beta2.GroupVersion.Group && or.Kind == ObjectReferenceTenantKind
}

func IsTenantOwnerReferenceForTenant(or metav1.OwnerReference, tnt *capsulev1beta2.Tenant) bool {
	if tnt == nil {
		return false
	}

	if or.Kind != ObjectReferenceTenantKind {
		return false
	}

	if or.APIVersion == "" {
		return false
	}

	parts := strings.Split(or.APIVersion, "/")
	if len(parts) != 2 {
		return false
	}

	group := parts[0]

	return group == capsulev1beta2.GroupVersion.Group && or.Kind == ObjectReferenceTenantKind && or.Name == tnt.GetName() && or.UID == tnt.GetUID()
}
