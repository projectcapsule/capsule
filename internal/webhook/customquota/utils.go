// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"k8s.io/apimachinery/pkg/api/resource"
)

func validateQuantity(q resource.Quantity) error {
	parsed, err := resource.ParseQuantity(q.String())
	if err != nil {
		return fmt.Errorf("invalid quantity %q: %w", q.String(), err)
	}

	if parsed.Sign() <= 0 {
		return fmt.Errorf("quantity must not be negative or 0: %q", q.String())
	}

	return nil
}

func sourceChanged(a, b capsulev1beta2.CustomQuotaSpecSource) bool {
	return a.GroupVersionKind.Group != b.GroupVersionKind.Group ||
		a.GroupVersionKind.Version != b.GroupVersionKind.Version ||
		a.GroupVersionKind.Kind != b.GroupVersionKind.Kind ||
		a.Path != b.Path
}
