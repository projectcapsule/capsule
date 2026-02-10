// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package template

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	"github.com/go-sprout/sprout/sprigin"
	"sigs.k8s.io/yaml"
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

// CustomFuncMap return our custom templates
func CustomFuncMap() template.FuncMap {
	return template.FuncMap{
		"toToml":            toTOML,
		"fromToml":          fromTOML,
		"fromYamlArray":     fromYAMLArray,
		"fromJsonArray":     fromJSONArray,
		"deterministicUUID": deterministicUUID,
	}
}

func deterministicUUID(parts ...string) string {
	// Normalize: trim whitespace; keep empty strings if caller passes them intentionally.
	// If you prefer to skip empties, filter them out here.
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		clean = append(clean, strings.TrimSpace(p))
	}

	// Deterministic stable separator. Using a non-printable delimiter reduces accidental collisions.
	// "|" is also fine; \x1f is "unit separator" and common for this use.
	msg := strings.Join(clean, "\x1f")

	sum := sha256.Sum256([]byte(msg))
	b := sum[:16] // 128-bit UUID material

	// Set RFC4122 variant (10xxxxxx)
	b[8] = (b[8] & 0x3f) | 0x80
	// Set version 5 (0101xxxx)
	b[6] = (b[6] & 0x0f) | 0x50

	// Format as 8-4-4-4-12 hex
	hex32 := hex.EncodeToString(b) // 32 lowercase hex chars
	uuid := hex32[0:8] + "-" + hex32[8:12] + "-" + hex32[12:16] + "-" + hex32[16:20] + "-" + hex32[20:32]
	return strings.ToUpper(uuid)
}

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
	b := bytes.NewBuffer(nil)
	e := toml.NewEncoder(b)

	err := e.Encode(v)
	if err != nil {
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
