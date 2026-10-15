// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package functions

import "testing"

func TestFuncMaps(t *testing.T) {
	t.Parallel()

	custom := CustomFuncMap()
	for _, name := range []string{
		"toToml",
		"fromToml",
		"fromYamlArray",
		"fromJsonArray",
		"deterministicUUID",
		"generateAgeKey",
		"generateAgePQKey",
		"getResourceByName",
		"mustGetResourceByName",
		"getResourceByNamespacedName",
	} {
		if custom[name] == nil {
			t.Fatalf("CustomFuncMap()[%q] is nil", name)
		}
	}

	extra := ExtraFuncMap()
	for _, name := range []string{"env", "expandenv", "expandEnv"} {
		if extra[name] != nil {
			t.Fatalf("ExtraFuncMap() exposes unsafe function %q", name)
		}
	}
	if extra["generateAgeKey"] == nil {
		t.Fatalf("ExtraFuncMap() missing custom function")
	}
}

func TestFromYAMLArray(t *testing.T) {
	t.Parallel()

	got := fromYAMLArray("- a\n- b\n")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fromYAMLArray() = %#v", got)
	}

	got = fromYAMLArray(":\n")
	if len(got) != 1 {
		t.Fatalf("fromYAMLArray(invalid) = %#v, want single error item", got)
	}
}

func TestGenerateAgeKeys(t *testing.T) {
	t.Parallel()

	key, ok := generateAgeKey().(AgeKeyPair)
	if !ok {
		t.Fatalf("generateAgeKey() did not return AgeKeyPair")
	}
	if key.Identity == "" || key.Recipient == "" {
		t.Fatalf("generateAgeKey() = %#v, want identity and recipient", key)
	}

	pqKey, ok := generateAgePQKey().(AgeKeyPair)
	if !ok {
		t.Fatalf("generateAgePQKey() did not return AgeKeyPair")
	}
	if pqKey.Identity == "" || pqKey.Recipient == "" {
		t.Fatalf("generateAgePQKey() = %#v, want identity and recipient", pqKey)
	}
}
