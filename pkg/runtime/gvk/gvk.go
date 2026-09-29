// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PreferredRESTMapping selects the first supported version that the server
// actually serves. Discover without version constraints first: a cold dynamic
// mapper can treat an absent requested version as a discovery failure, even
// when another requested version is available.
func PreferredRESTMapping(mapper meta.RESTMapper, kind schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	mappings, err := mapper.RESTMappings(kind)
	if err != nil {
		return nil, err
	}

	for _, version := range versions {
		for _, mapping := range mappings {
			if mapping.GroupVersionKind.Version == version {
				return mapping, nil
			}
		}
	}

	return nil, &meta.NoKindMatchError{GroupKind: kind, SearchedVersions: versions}
}

func HasGVK(mapper meta.RESTMapper, gvk schema.GroupVersionKind) bool {
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return false
		}

		ctrl.Log.WithName("gvk-check").Error(err, "failed to check RESTMapping", "gvk", gvk.String())

		return false
	}

	return true
}
