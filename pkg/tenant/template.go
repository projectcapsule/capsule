// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// NewTenantContext returns the context for the tenant.
func NewTenantContext(tnt *capsulev1beta2.Tenant, scheme *runtime.Scheme, opts sanitize.SanitizeOptions) (context map[string]any, err error) {
	// initialize context
	context = map[string]any{}

	if err := sanitize.SanitizeObject(tnt, scheme, opts); err != nil {
		return nil, err
	}

	context, err = utils.ToUnstructuredMap(tnt)
	if err != nil {
		return nil, err
	}

	roles := tnt.GetClusterRolesBySubject(nil)

	context["rbac"] = roles

	return context, nil
}

// TemplateForTenantAndNamespace applies templatingto the provided string.
func FastContextForTenantAndNamespace(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) map[string]string {
	values := map[string]string{}

	if tnt != nil {
		values["tenant.name"] = tnt.Name
	}

	if ns != nil {
		values["namespace"] = ns.Name
	}

	return values
}
