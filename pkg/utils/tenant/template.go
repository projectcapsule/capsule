// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// TemplateForTenantAndNamespace applies templatingto the provided string.
func ContextForTenantAndNamespace(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) map[string]string {
	values := map[string]string{}

	if tnt != nil {
		values["tenant.name"] = tnt.Name
	}

	if ns != nil {
		values["namespace"] = ns.Name
	}

	return values
}
