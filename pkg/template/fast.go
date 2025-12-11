// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"io"
	"strings"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// TemplateForTenantAndNamespace applies templatingto the provided string.
func TemplateForTenantAndNamespace(template string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) string {
	if !strings.Contains(template, "{{") && !strings.Contains(template, "}}") {
		return template
	}

	t := fasttemplate.New(template, "{{", "}}")

	values := map[string]string{
		"tenant.name": tnt.Name,
		"namespace":   ns.Name,
	}

	return t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		key := strings.TrimSpace(tag)
		if v, ok := values[key]; ok {
			return w.Write([]byte(v))
		}

		return 0, nil
	})
}

// TemplateForTenantAndNamespace applies templating to all values in the provided map in place.
func TemplateForTenantAndNamespaceMap(m map[string]string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) {
	for k, v := range m {
		m[k] = TemplateForTenantAndNamespace(v, tnt, ns)
	}
}
