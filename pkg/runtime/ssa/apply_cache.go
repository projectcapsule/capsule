// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"crypto/sha256"
	"encoding/json"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyCache only remembers completed writes. A hit requires a fresh target read
// under the applying identity; it never substitutes for authorization or ownership
// checks. Implementations must bound retention and copy mutable result fields.
type ApplyCache interface {
	Get(key ApplyCacheKey, uid types.UID, version string) (ApplyResult, bool)
	Set(key ApplyCacheKey, uid types.UID, version string, result ApplyResult)
}

// ApplyCacheKey separates client/credential lifetimes and the entire rendered
// intent, including lifecycle policy. Digests remain process-local, including for
// Secrets; they must never be exposed in status, metrics or logs.
type ApplyCacheKey struct {
	Identity any
	Intent   [sha256.Size]byte
}

func (m Manager) applyCacheKey(c client.Client, desired *unstructured.Unstructured, opts ApplyOptions) (ApplyCacheKey, bool) {
	if m.Applied == nil || opts.DryRun {
		return ApplyCacheKey{}, false
	}

	var identity any = c
	// Constructors can identify one credential lifetime across API budgets.
	// Never derive this identity from user-supplied labels or manifests.
	if identified, ok := c.(interface{ ApplyCacheIdentity() any }); ok {
		if key := identified.ApplyCacheIdentity(); key != nil {
			identity = key
		}
	}

	if !reflect.ValueOf(identity).Comparable() {
		return ApplyCacheKey{}, false
	}

	data, err := json.Marshal(struct { //nolint:musttag // Options and metadata are serialized only for a process-local fingerprint.
		Object   map[string]any `json:"object"`
		Options  ApplyOptions   `json:"options"`
		Metadata Metadata       `json:"metadata"`
	}{desired.Object, opts, m.Metadata})
	if err != nil {
		return ApplyCacheKey{}, false
	}

	return ApplyCacheKey{Identity: identity, Intent: sha256.Sum256(data)}, true
}

func (m Manager) cachedApply(key ApplyCacheKey, cacheable bool, existing *unstructured.Unstructured) (ApplyResult, bool) {
	if !cacheable || existing == nil {
		return ApplyResult{}, false
	}

	result, hit := m.Applied.Get(key, existing.GetUID(), existing.GetResourceVersion())
	result.ContentChanged = false

	return result, hit
}

func (m Manager) recordApply(key ApplyCacheKey, cacheable bool, actual *unstructured.Unstructured, result ApplyResult) {
	if cacheable {
		m.Applied.Set(key, actual.GetUID(), actual.GetResourceVersion(), result)
	}
}
