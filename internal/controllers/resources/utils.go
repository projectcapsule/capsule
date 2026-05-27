// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"hash/fnv"
	"strconv"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func getFieldOwner(name string, namespace string) string {
	if namespace == "" {
		namespace = "Cluster"
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(name))

	return strconv.FormatUint(h.Sum64(), 36)
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
