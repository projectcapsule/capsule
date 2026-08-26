// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import "fmt"

func getResourceByName(name string, resources any) (map[string]any, error) {
	return findResource(
		resources,
		fmt.Sprintf("metadata.name %q", name),
		func(metadata map[string]any) bool {
			return metadata["name"] == name
		},
	)
}

func mustGetResourceByName(name string, resources any) (map[string]any, error) {
	resource, err := getResourceByName(name, resources)
	if err != nil {
		return nil, err
	}

	if len(resource) == 0 {
		return nil, fmt.Errorf("resource with metadata.name %q was not found", name)
	}

	return resource, nil
}

func getResourceByNamespacedName(namespace, name string, resources any) (map[string]any, error) {
	return findResource(
		resources,
		fmt.Sprintf("metadata.namespace %q and metadata.name %q", namespace, name),
		func(metadata map[string]any) bool {
			resourceNamespace, _ := metadata["namespace"].(string)

			return resourceNamespace == namespace && metadata["name"] == name
		},
	)
}

func findResource(
	resources any,
	description string,
	matches func(map[string]any) bool,
) (map[string]any, error) {
	items, err := resourceItems(resources)
	if err != nil {
		return nil, err
	}

	var match map[string]any

	for index, item := range items {
		metadata, ok := item["metadata"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("resource at index %d has invalid or missing metadata", index)
		}

		if !matches(metadata) {
			continue
		}

		if match != nil {
			return nil, fmt.Errorf("multiple resources match %s", description)
		}

		match = item
	}

	if match == nil {
		return map[string]any{}, nil
	}

	return match, nil
}

func resourceItems(resources any) ([]map[string]any, error) {
	switch value := resources.(type) {
	case nil:
		return nil, nil
	case []map[string]any:
		return value, nil
	case []any:
		items := make([]map[string]any, 0, len(value))

		for index, item := range value {
			resource, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("resource at index %d must be an object, got %T", index, item)
			}

			items = append(items, resource)
		}

		return items, nil
	default:
		return nil, fmt.Errorf("resources must be a list of objects, got %T", resources)
	}
}
