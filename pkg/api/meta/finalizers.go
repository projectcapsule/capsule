// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import "encoding/json"

const (
	ControllerFinalizer     = "controller.projectcapsule.dev/finalize"
	LegacyResourceFinalizer = "capsule.clastix.io/resources"
)

var EmptyFinalizersMergePatch = []byte(`{"metadata":{"finalizers":[]}}`)

func FilterFinalizers(finalizers []string, ignored map[string]struct{}) (remaining []string, removed bool) {
	if len(finalizers) == 0 {
		return nil, false
	}

	if len(ignored) == 0 {
		return nil, true
	}

	remaining = make([]string, 0, len(finalizers))

	removed = false

	for _, f := range finalizers {
		if _, ok := ignored[f]; ok {
			remaining = append(remaining, f)
			continue
		}

		removed = true
	}

	if len(remaining) == 0 {
		return nil, removed
	}

	return remaining, removed
}

func BuildFinalizersMergePatch(finalizers []string) []byte {
	if len(finalizers) == 0 {
		return EmptyFinalizersMergePatch
	}

	type metadata struct {
		Finalizers []string `json:"finalizers"`
	}

	type patch struct {
		Metadata metadata `json:"metadata"`
	}

	raw, _ := json.Marshal(patch{
		Metadata: metadata{
			Finalizers: finalizers,
		},
	})

	return raw
}
