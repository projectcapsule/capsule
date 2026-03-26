// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"fmt"
	"strings"

	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func ParseQuantities(value string) (resource.Quantity, error) {
	fields := strings.Fields(value)

	total := resource.Quantity{}

	for _, v := range fields {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			return total, fmt.Errorf("invalid quantity %q: %w", v, err)
		}

		total.Add(q)
	}

	return total, nil
}

// GetUsageFromUnstructured extracts a value from an unstructured object using a JSONPath source path.
// It is convenient for one-off calls. For repeated calls with the same sourcePath, prefer
// CompileUsageJSONPath(...) and then Execute(...) to avoid reparsing.
func ParseUsageFromUnstructured(u unstructured.Unstructured, sourcePath string) (string, error) {
	compiled, err := clt.CompileUsageJSONPath(sourcePath)
	if err != nil {
		return "", err
	}

	return compiled.Execute(u)
}

func ParseQuantityFromUnstructured(u unstructured.Unstructured, sourcePath string) (resource.Quantity, error) {
	compiled, err := ParseUsageFromUnstructured(u, sourcePath)
	if err != nil {
		return resource.Quantity{}, err
	}

	return ParseQuantities(compiled)
}
