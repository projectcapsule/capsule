// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

func ValidateQuantity(q resource.Quantity) error {
	parsed, err := resource.ParseQuantity(q.String())
	if err != nil {
		return fmt.Errorf("invalid quantity %q: %w", q.String(), err)
	}

	if parsed.Sign() <= 0 {
		return fmt.Errorf("quantity must not be negative or 0: %q", q.String())
	}

	return nil
}

func ClampQuantityToZero(q *resource.Quantity) {
	if q.Sign() < 0 {
		*q = resource.MustParse("0")
	}
}

func NegateQuantity(in resource.Quantity) resource.Quantity {
	out := in.DeepCopy()
	out.Neg()

	return out
}
