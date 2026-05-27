// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package functions

import (
	"bytes"
	"encoding/json"

	"github.com/BurntSushi/toml"
	"sigs.k8s.io/yaml"
)

// fromYAMLArray converts a YAML array into a []interface{}.
//
// This is not a general-purpose YAML parser, and will not parse all valid
// YAML documents. Additionally, because its intended use is within templates
// it tolerates errors. It will insert the returned error message string as
// the first and only item in the returned array.
func fromYAMLArray(str string) []any {
	a := []any{}

	if err := yaml.Unmarshal([]byte(str), &a); err != nil {
		a = []any{err.Error()}
	}

	return a
}

// toTOML takes an interface, marshals it to toml, and returns a string. It will
// always return a string, even on marshal error (empty string).
//
// This is designed to be called from a template.
func toTOML(v any) string {
	if v == nil {
		return ""
	}

	b := bytes.NewBuffer(nil)
	e := toml.NewEncoder(b)

	if err := e.Encode(v); err != nil {
		return err.Error()
	}

	return b.String()
}

// fromTOML converts a TOML document into a map[string]interface{}.
//
// This is not a general-purpose TOML parser, and will not parse all valid
// TOML documents. Additionally, because its intended use is within templates
// it tolerates errors. It will insert the returned error message string into
// m["Error"] in the returned map.
func fromTOML(str string) map[string]any {
	m := make(map[string]any)

	if err := toml.Unmarshal([]byte(str), &m); err != nil {
		m["Error"] = err.Error()
	}

	return m
}

// fromJSONArray converts a JSON array into a []interface{}.
//
// This is not a general-purpose JSON parser, and will not parse all valid
// JSON documents. Additionally, because its intended use is within templates
// it tolerates errors. It will insert the returned error message string as
// the first and only item in the returned array.
func fromJSONArray(str string) []any {
	a := []any{}

	if err := json.Unmarshal([]byte(str), &a); err != nil {
		a = []any{err.Error()}
	}

	return a
}
