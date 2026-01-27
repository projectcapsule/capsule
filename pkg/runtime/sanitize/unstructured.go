// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package sanitize

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// SanitizeUnstructured Removes additional metadata we might not need when loading unstructured items into a context
func SanitizeUnstructured(obj *unstructured.Unstructured, opts SanitizeOptions) {
	if obj == nil {
		return
	}

	if opts.StripUID {
		unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	}

	if opts.StripManagedFields {
		unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	}

	if opts.StripLastApplied {
		anns, found, err := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
		if err == nil && found && len(anns) > 0 {
			// kubectl apply annotation
			delete(anns, "kubectl.kubernetes.io/last-applied-configuration")

			// (Optional) If you also want to strip other common “apply-ish” annotations, uncomment:
			// delete(anns, "kubernetes.io/change-cause")

			if len(anns) == 0 {
				unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")
			} else {
				_ = unstructured.SetNestedStringMap(obj.Object, anns, "metadata", "annotations")
			}
		}
	}

	if opts.StripStatus {
		unstructured.RemoveNestedField(obj.Object, "status")
	}
}
