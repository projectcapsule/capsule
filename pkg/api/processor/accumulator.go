// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/api/misc"
)

// Keeps track of generated items
type Accumulator = map[string]*AccumulatorItem

// Keeps track of generated items
type AccumulatorItem struct {
	Resource misc.ResourceID
	Objects  *[]AccumulatorObject
}

// Keeps track of generated items
type AccumulatorObject struct {
	Origin misc.TenantResourceIDWithOrigin
	Object *unstructured.Unstructured
}

func AccumulatorAdd(
	acc Accumulator,
	resource misc.ResourceID,
	obj AccumulatorObject,
) {
	if acc == nil {
		return
	}

	key := resource.GetKey("")

	if entry, ok := acc[key]; ok && entry != nil {
		if entry.Objects == nil {
			list := make([]AccumulatorObject, 0, 1)
			entry.Objects = &list
		}
		*entry.Objects = append(*entry.Objects, obj)
		return
	}

	list := []AccumulatorObject{obj}

	acc[key] = &AccumulatorItem{
		Resource: resource,
		Objects:  &list,
	}
}
