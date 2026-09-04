// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import "fmt"

// walkJSONSchemaNodes visits schema objects recursively. It covers the schema
// locations supported by JSON Schema 2020-12 and is shared by Capsule's form
// and validation vendor extensions.
func walkJSONSchemaNodes(
	node any,
	path string,
	visit func(map[string]any, string) error,
) error {
	schemaNode, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	if err := visit(schemaNode, path); err != nil {
		return err
	}

	for _, keyword := range []string{
		"additionalProperties",
		"contains",
		"contentSchema",
		"else",
		"if",
		"items",
		"not",
		"propertyNames",
		"then",
		"unevaluatedItems",
		"unevaluatedProperties",
	} {
		if child, found := schemaNode[keyword]; found {
			if err := walkJSONSchemaNodes(child, path+"."+keyword, visit); err != nil {
				return err
			}
		}
	}

	for _, keyword := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		children, ok := schemaNode[keyword].([]any)
		if !ok {
			continue
		}

		for index, child := range children {
			if err := walkJSONSchemaNodes(child, fmt.Sprintf("%s.%s[%d]", path, keyword, index), visit); err != nil {
				return err
			}
		}
	}

	for _, keyword := range []string{"$defs", "definitions", "dependentSchemas", "patternProperties", "properties"} {
		children, ok := schemaNode[keyword].(map[string]any)
		if !ok {
			continue
		}

		for name, child := range children {
			if err := walkJSONSchemaNodes(child, path+"."+keyword+"."+name, visit); err != nil {
				return err
			}
		}
	}

	return nil
}
