// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk

import (
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NamespacedListableResources selects resources supported by namespace cleanup.
// Cleanup lists and deletes objects, then gets and patches objects with finalizers.
func NamespacedListableResources(resourceLists []*metav1.APIResourceList) ([]schema.GroupVersionResource, error) {
	gvrs := make([]schema.GroupVersionResource, 0, 64)
	seen := make(map[schema.GroupVersionResource]struct{})

	for _, rl := range resourceLists {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("parse groupVersion %q: %w", rl.GroupVersion, err)
		}

		for _, r := range rl.APIResources {
			if !r.Namespaced {
				continue
			}

			if strings.Contains(r.Name, "/") {
				continue
			}

			if !SupportsVerb(r.Verbs, "list") || !SupportsVerb(r.Verbs, "delete") {
				continue
			}

			if !SupportsVerb(r.Verbs, "get") || !SupportsVerb(r.Verbs, "patch") {
				continue
			}

			gvr := gv.WithResource(r.Name)
			if _, ok := seen[gvr]; ok {
				continue
			}

			seen[gvr] = struct{}{}

			gvrs = append(gvrs, gvr)
		}
	}

	return gvrs, nil
}

func SupportsVerb(verbs metav1.Verbs, want string) bool {
	return slices.Contains(verbs, want)
}
