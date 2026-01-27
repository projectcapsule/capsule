// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize

import (
	"fmt"

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SanitizeObject removes metadata (and optionally status) from a client.Object in-place.
// For StripStatus it converts to unstructured and back (generic, but only when needed).
func SanitizeObject(obj client.Object, scheme *runtime.Scheme, opts SanitizeOptions) error {
	if obj == nil {
		return nil
	}

	if opts.StripUID {
		obj.SetUID("")
	}

	if opts.StripManagedFields {
		accessor, err := apiMeta.Accessor(obj)
		if err == nil {
			accessor.SetManagedFields(nil)
		}
	}

	if opts.StripLastApplied {
		anns := obj.GetAnnotations()
		if len(anns) > 0 {
			delete(anns, "kubectl.kubernetes.io/last-applied-configuration")
			if len(anns) == 0 {
				obj.SetAnnotations(nil)
			} else {
				obj.SetAnnotations(anns)
			}
		}
	}

	if opts.StripStatus {
		if scheme == nil {
			return fmt.Errorf("scheme is required to StripStatus on typed objects")
		}

		// Convert typed -> unstructured
		u := &unstructured.Unstructured{}
		if err := scheme.Convert(obj, u, nil); err != nil {
			m, err2 := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err2 != nil {
				return fmt.Errorf("failed converting object to unstructured for status stripping: %w", err)
			}

			u.Object = m
		}

		unstructured.RemoveNestedField(u.Object, "status")

		// Convert back unstructured -> typed
		if err := scheme.Convert(u, obj, nil); err != nil {
			if err2 := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err2 != nil {
				return fmt.Errorf("failed converting unstructured back to typed after status stripping: %w", err2)
			}
		}
	}

	return nil
}
