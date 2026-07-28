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

func (c *CustomQuotaSpec) CollectCELExpressions() (expressions []string) {
	set := map[string]struct{}{}

	for _, source := range c.Sources {
		if source.CEL != "" {
			set[source.CEL] = struct{}{}
		}

		for _, sel := range source.Selectors {
			for _, expression := range sel.CELExpressions {
				if expression != "" {
					set[expression] = struct{}{}
				}
			}
		}
	}

	for expression := range set {
		expressions = append(expressions, expression)
	}

	return expressions
}
