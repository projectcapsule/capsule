// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"strings"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// TemplateForTenantAndNamespace applies templating to all values in the provided map in place.
func TemplateForTenantAndNamespace(m map[string]string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) {
	for k, v := range m {
		if !strings.Contains(v, "{{ ") && !strings.Contains(v, " }}") {
			continue
		}

		t := fasttemplate.New(v, "{{ ", " }}")
		tmplString := t.ExecuteString(map[string]interface{}{
			"tenant.name": tnt.Name,
			"namespace":   ns.Name,
		})

		m[k] = tmplString
	}
}
