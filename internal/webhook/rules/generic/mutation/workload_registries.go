// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import corev1 "k8s.io/api/core/v1"

// Merge references by name, keeping each first occurrence in order. Copy only if
// existing duplicates must be removed, so admission's original snapshot stays intact.
func mergeImagePullSecrets(existing, configured []corev1.LocalObjectReference) []corev1.LocalObjectReference {
	if configured == nil {
		return existing
	}

	var unique []corev1.LocalObjectReference

	known := make(map[string]struct{}, len(existing)+len(configured))
	for i, secret := range existing {
		if _, found := known[secret.Name]; found {
			if unique == nil {
				unique = make([]corev1.LocalObjectReference, i, len(existing)+len(configured))
				copy(unique, existing[:i])
			}

			continue
		}

		known[secret.Name] = struct{}{}

		if unique != nil {
			unique = append(unique, secret)
		}
	}

	if unique != nil {
		existing = unique
	}

	for _, secret := range configured {
		if _, found := known[secret.Name]; found {
			continue
		}

		existing = append(existing, secret)
		known[secret.Name] = struct{}{}
	}

	return existing
}
