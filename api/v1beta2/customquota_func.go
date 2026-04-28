// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

func (c *CustomQuotaSpec) CollectJSONPathExpressions() (expressions []string) {
	set := map[string]struct{}{}

	for _, source := range c.Sources {
		if source.Path != "" {
			set[source.Path] = struct{}{}
		}

		for _, sel := range source.Selectors {
			for _, fs := range sel.FieldSelectors {
				if fs != "" {
					set[fs] = struct{}{}
				}
			}
		}
	}

	for e := range set {
		expressions = append(expressions, e)
	}

	return expressions
}
