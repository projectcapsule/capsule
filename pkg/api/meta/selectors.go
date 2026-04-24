// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

var WithoutCapsuleManagedResourcesLabelSelector = func() string {
	req, _ := labels.NewRequirement(
		NewManagedByCapsuleLabel,
		selection.NotIn,
		[]string{
			ValueController,
			ValueControllerResources,
		},
	)

	return labels.NewSelector().Add(*req).String()
}()

var WithCapsuleManagedResourcesLabelSelector = func() string {
	req, _ := labels.NewRequirement(
		NewManagedByCapsuleLabel,
		selection.In,
		[]string{
			ValueController,
			ValueControllerResources,
		},
	)

	return labels.NewSelector().Add(*req).String()
}()
