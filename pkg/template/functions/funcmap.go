// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package functions

import (
	"maps"
	"text/template"

	"github.com/go-sprout/sprout/sprigin"
)

// TxtFuncMap returns an aggregated template function map. Currently (custom functions + sprig).
func ExtraFuncMap() template.FuncMap {
	funcMap := sprigin.FuncMap()

	maps.Copy(funcMap, CustomFuncMap())

	// Remove unsafe methods
	delete(funcMap, "env")
	delete(funcMap, "expandEnv")

	return funcMap
}

// CustomFuncMap return our custom templates.
func CustomFuncMap() template.FuncMap {
	return template.FuncMap{
		"toToml":            toTOML,
		"fromToml":          fromTOML,
		"fromYamlArray":     fromYAMLArray,
		"fromJsonArray":     fromJSONArray,
		"deterministicUUID": deterministicUUID,
		"generateAgeKey":    generateAgeKey,
		"generateAgePQKey":  generateAgePQKey,
	}
}
