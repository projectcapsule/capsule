// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceKey struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

// keyFromUnstructured builds a stable identity for dedupe.
// Prefer UID if you want “same object even if renamed” semantics; for Kubernetes resources
// name+namespace+GVK is typically what you want for “don’t process duplicates”.
func KeyFromUnstructured(o *unstructured.Unstructured) (ResourceKey, bool) {
	if o == nil {
		return ResourceKey{}, false
	}

	gvk := o.GroupVersionKind()
	if gvk.Empty() {
		gvk.Kind = o.GetKind()
		gv, err := schema.ParseGroupVersion(o.GetAPIVersion())
		if err == nil {
			gvk.Group = gv.Group
			gvk.Version = gv.Version
		}
	}

	if gvk.Kind == "" || o.GetName() == "" {
		return ResourceKey{}, false
	}

	return ResourceKey{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: o.GetNamespace(),
		Name:      o.GetName(),
	}, true
}
