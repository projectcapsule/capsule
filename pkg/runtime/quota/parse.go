// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
)

func ParseBoolFromUnstructured(u unstructured.Unstructured, compiled *jsonpath.CompiledJSONPath) (bool, error) {
	value, err := ParseUsageFromUnstructured(u, compiled)
	if err != nil {
		return false, err
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("condition path %q did not resolve to a boolean, got %q: %w", compiled, value, err)
	}

	return parsed, nil
}

func ConditionsMatch(u unstructured.Unstructured, conditions []*jsonpath.CompiledJSONPath) (bool, error) {
	for _, cond := range conditions {
		ok, err := ParseBoolFromUnstructured(u, cond)
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}

func ParseQuantities(value string) (resource.Quantity, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return resource.Quantity{}, fmt.Errorf("no quantity values found")
	}

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
func ParseUsageFromUnstructured(u unstructured.Unstructured, compiled *jsonpath.CompiledJSONPath) (string, error) {
	return compiled.Execute(u)
}

func ParseQuantityFromUnstructured(u unstructured.Unstructured, compiled *jsonpath.CompiledJSONPath) (resource.Quantity, error) {
	usage, err := ParseUsageFromUnstructured(u, compiled)
	if err != nil {
		return resource.Quantity{}, err
	}

	if strings.TrimSpace(usage) == "" {
		return resource.Quantity{}, fmt.Errorf("quantity path did not resolve to any value")
	}

	return ParseQuantities(usage)
}
