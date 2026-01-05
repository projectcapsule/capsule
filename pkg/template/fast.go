// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"io"
	"maps"
	"strings"

	"github.com/valyala/fasttemplate"
)

// TemplateForTenantAndNamespace applies templating to the provided string.
func TemplateForTenantAndNamespace(
	template string,
	templateContext map[string]string,
) string {
	if !strings.Contains(template, "{{") && !strings.Contains(template, "}}") {
		return template
	}

	t := fasttemplate.New(template, "{{", "}}")

	return t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		key := strings.TrimSpace(tag)
		if v, ok := templateContext[key]; ok {
			return w.Write([]byte(v))
		}

		return 0, nil
	})
}

// TemplateForTenantAndNamespace applies templating to all values in the provided map in place.
func TemplateForTenantAndNamespaceMap(
	m map[string]string,
	templateContext map[string]string,
) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}

	out := maps.Clone(m)
	for k, v := range out {
		out[k] = TemplateForTenantAndNamespace(v, templateContext)
	}

	return out
}
