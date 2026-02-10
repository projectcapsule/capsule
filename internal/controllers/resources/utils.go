// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func getFieldOwner(name string, namespace string) string {
	if namespace == "" {
		namespace = "cluster"
	}

	return meta.FieldManagerCapsulePrefix + "/" + "resource" + "/" + namespace + "/" + name + "/"
}

func getSelectorForCreatedResourcesExclusion() (labels.Selector, error) {
	selector := labels.NewSelector()

	req, err := labels.NewRequirement(
		meta.CreatedByCapsuleLabel,
		selection.NotIn,
		[]string{meta.ValueControllerResources},
	)

	if err != nil {
		return nil, err
	}

	selector.Add(*req)

	return selector, nil
}
