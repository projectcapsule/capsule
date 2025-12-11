// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// TemplateForTenantAndNamespace applies templatingto the provided string.
func TemplateForTenantAndNamespace(template string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) string {
	if !strings.Contains(template, "{{") && !strings.Contains(template, "}}") {
		return ""
	}

	t := fasttemplate.New(template, "{{", "}}")

	return t.ExecuteString(map[string]any{
		"tenant.name": tnt.Name,
		"namespace":   ns.Name,
	})
}

// TemplateForTenantAndNamespace applies templating to all values in the provided map in place.
func TemplateForTenantAndNamespaceMap(m map[string]string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) {
	for k, v := range m {
		m[k] = TemplateForTenantAndNamespace(v, tnt, ns)
	}
}
