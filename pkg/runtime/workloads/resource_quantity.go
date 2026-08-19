// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"fmt"

	inf "gopkg.in/inf.v0"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RatioSupportedResource reports whether Capsule can safely calculate a
// limit-to-request ratio for the resource. CPU is rounded down to milliCPU;
// byte-based resources are rounded down to whole bytes.
func RatioSupportedResource(name corev1.ResourceName) bool {
	return name == corev1.ResourceCPU ||
		name == corev1.ResourceMemory ||
		name == corev1.ResourceEphemeralStorage
}

// LimitForRatio calculates request * ratio without floating-point arithmetic.
// The result is rounded down so it never exceeds the configured maximum ratio.
func LimitForRatio(
	name corev1.ResourceName,
	request resource.Quantity,
	ratio resource.Quantity,
) (resource.Quantity, error) {
	if !RatioSupportedResource(name) {
		return resource.Quantity{}, fmt.Errorf("ratio is not supported for resource %q", name)
	}

	if request.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("request for resource %q must be greater than zero", name)
	}

	if ratio.Cmp(resource.MustParse("1")) < 0 {
		return resource.Quantity{}, fmt.Errorf("ratio for resource %q must be greater than or equal to 1", name)
	}

	product := new(inf.Dec).Mul(request.AsDec(), ratio.AsDec())

	scale := inf.Scale(0)

	if name == corev1.ResourceCPU {
		scale = inf.Scale(3)
	}

	rounded := new(inf.Dec).Round(product, scale, inf.RoundDown)

	return *resource.NewDecimalQuantity(*rounded, request.Format), nil
}
