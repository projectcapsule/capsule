// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ValidateHardLimit rejects removal or reduction of a hard resource below an
// already allocated quantity. Path identifies the hard-limit field in the
// returned validation error.
func ValidateHardLimit(path string, hard, allocated corev1.ResourceList) error {
	for name, usage := range allocated {
		if usage.Sign() <= 0 {
			continue
		}

		limit, exists := hard[name]
		if !exists {
			return fmt.Errorf(
				"%s[%q] cannot be removed while %s is allocated",
				path,
				name,
				usage.String(),
			)
		}

		if limit.Cmp(usage) < 0 {
			return fmt.Errorf(
				"%s[%q] cannot be reduced to %s while %s is allocated",
				path,
				name,
				limit.String(),
				usage.String(),
			)
		}
	}

	return nil
}
