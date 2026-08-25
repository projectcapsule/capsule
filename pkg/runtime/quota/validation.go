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

// ValidateHardLimitScopeChange prevents a quota from reducing or removing a
// hard limit in the same update that changes its namespace selection. Usage
// from newly selected namespaces is not represented by the quota's previous
// status yet, so the scope must reconcile before a safe lower bound is known.
func ValidateHardLimitScopeChange(
	path string,
	hard corev1.ResourceList,
	previous corev1.ResourceList,
	scopeChanged bool,
) error {
	if !scopeChanged {
		return nil
	}

	for name, previousLimit := range previous {
		limit, exists := hard[name]
		if !exists {
			return fmt.Errorf(
				"%s[%q] cannot be removed while namespace selectors are changing; update the selectors first and wait for usage reconciliation",
				path,
				name,
			)
		}

		if limit.Cmp(previousLimit) < 0 {
			return fmt.Errorf(
				"%s[%q] cannot be reduced from %s to %s while namespace selectors are changing; update the selectors first and wait for usage reconciliation",
				path,
				name,
				previousLimit.String(),
				limit.String(),
			)
		}
	}

	return nil
}
