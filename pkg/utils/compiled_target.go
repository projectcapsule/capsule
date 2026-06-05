// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func CompileFieldSelector(
	cache *cache.JSONPathCache,
	raw string,
) (selectors.CompiledFieldSelector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return selectors.CompiledFieldSelector{}, fmt.Errorf("field selector must not be empty")
	}

	path, value, ok := jsonpath.SplitFieldSelectorEquals(raw)
	if !ok {
		compiledPath, err := cache.GetOrCompile(raw)
		if err != nil {
			return selectors.CompiledFieldSelector{}, err
		}

		return selectors.CompiledFieldSelector{
			Raw:      raw,
			Path:     raw,
			Operator: selectors.FieldSelectorTruthy,
			Compiled: compiledPath,
		}, nil
	}

	compiledPath, err := cache.GetOrCompile(path)
	if err != nil {
		return selectors.CompiledFieldSelector{}, err
	}

	return selectors.CompiledFieldSelector{
		Raw:      raw,
		Path:     path,
		Operator: selectors.FieldSelectorEquals,
		Value:    value,
		Compiled: compiledPath,
	}, nil
}
