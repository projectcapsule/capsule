// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// NewTenantContext returns the context for the tenant.
func NewTenantContext(tnt *capsulev1beta2.Tenant, _ *runtime.Scheme, opts sanitize.SanitizeOptions) (map[string]any, error) {
	context, err := utils.ToUnstructuredMap(tnt)
	if err != nil {
		return nil, err
	}

	sanitize.SanitizeUnstructured(&unstructured.Unstructured{Object: context}, opts)

	context["rbac"] = tnt.GetClusterRolesBySubject(nil)

	return context, nil
}

// NewTenantNamespaceContext returns the context for the tenant and a given namespace.
func NewTenantNamespaceContext(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	scheme *runtime.Scheme,
	opts sanitize.SanitizeOptions,
) (map[string]any, error) {
	context := make(map[string]any)

	if tnt != nil {
		tCtx, err := NewTenantContext(tnt, scheme, opts)
		if err != nil {
			return context, err
		}

		context["tenant"] = tCtx
	}

	if ns != nil {
		nsMap, err := utils.ToUnstructuredMap(ns)
		if err != nil {
			return context, err
		}

		sanitize.SanitizeUnstructured(&unstructured.Unstructured{Object: nsMap}, opts)

		context["namespace"] = nsMap
	}

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
