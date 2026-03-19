// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"sort"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

type ProcessedItems []ObjectReferenceStatus

// Adds a condition by type.
func (p *ProcessedItems) UpdateItem(item ObjectReferenceStatus) {
	for i, stat := range *p {
		if p.isEqual(stat, item) {
			(*p)[i].ObjectReferenceStatusCondition = item.ObjectReferenceStatusCondition

			return
		}
	}

	*p = append(*p, item)
}

// Removes a condition by type.
func (p *ProcessedItems) RemoveItem(item ObjectReferenceStatus) {
	filtered := make(ProcessedItems, 0, len(*p))

	for _, stat := range *p {
		if !p.isEqual(stat, item) {
			filtered = append(filtered, stat)
		}
	}

	*p = filtered
}

// Removes a condition by type.
// Returns actual item pointer, not a copy
func (p *ProcessedItems) GetItem(ref gvk.ResourceID) *ObjectReferenceStatus {
	for i := range *p {
		if (*p)[i].ResourceID == ref {
			return &(*p)[i]
		}
	}

	return nil
}

func (p *ProcessedItems) isEqual(a, b ObjectReferenceStatus) bool {
	return a.ResourceID == b.ResourceID
}

func (p ProcessedItems) SortDeterministic() {
	sort.Slice(p, func(i, j int) bool {
		a, b := p[i], p[j]

		if a.Tenant != b.Tenant {
			return a.Tenant < b.Tenant
		}

		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}

		if a.Name != b.Name {
			return a.Name < b.Name
		}

		return a.Kind < b.Kind
	})
}
