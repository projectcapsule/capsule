// Copyright 2020-2026 Project Capsule Authors
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

// InScope returns a copy of the items replicated into the given Tenant Namespace.
// The result shares no backing array with the receiver, thus it can be freely
// mutated through UpdateItem or RemoveItem.
func (p ProcessedItems) InScope(tenant, namespace string) ProcessedItems {
	scoped := make(ProcessedItems, 0, len(p))

	for _, stat := range p {
		if !isInScope(stat, tenant, namespace) {
			continue
		}

		scoped = append(scoped, stat)
	}

	return scoped
}

// ReplaceScope swaps the items belonging to the given Tenant Namespace with the provided
// ones, leaving the items of any other scope untouched: this allows a Namespace scoped
// reconciliation to report its outcome without dropping what has been processed for
// the remaining Namespaces.
//
// Items which are already tracked keep their position to avoid pointless status
// churn, whereas the ones missing from the given list are dropped, since the scope
// is no longer processing them. Provided items out of the given scope are ignored.
func (p *ProcessedItems) ReplaceScope(tenant, namespace string, items ProcessedItems) {
	replacements := make(map[gvk.ResourceID]ObjectReferenceStatusCondition, len(items))

	for _, item := range items {
		if !isInScope(item, tenant, namespace) {
			continue
		}

		replacements[item.ResourceID] = item.ObjectReferenceStatusCondition
	}

	filtered := make(ProcessedItems, 0, len(*p)+len(replacements))

	for _, stat := range *p {
		if !isInScope(stat, tenant, namespace) {
			filtered = append(filtered, stat)

			continue
		}

		condition, ok := replacements[stat.ResourceID]
		if !ok {
			continue
		}

		stat.ObjectReferenceStatusCondition = condition

		filtered = append(filtered, stat)

		delete(replacements, stat.ResourceID)
	}

	// Appending in the provided order the items which were not yet tracked.
	for _, item := range items {
		if _, ok := replacements[item.ResourceID]; !ok {
			continue
		}

		filtered = append(filtered, item)

		delete(replacements, item.ResourceID)
	}

	*p = filtered
}

// Removes a condition by type.
// Returns actual item pointer, not a copy.
func (p *ProcessedItems) GetItem(ref gvk.ResourceID) *ObjectReferenceStatus {
	for i := range *p {
		if (*p)[i].ResourceID == ref {
			return &(*p)[i]
		}
	}

	return nil
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

func (p *ProcessedItems) isEqual(a, b ObjectReferenceStatus) bool {
	return a.ResourceID == b.ResourceID
}

func isInScope(item ObjectReferenceStatus, tenant, namespace string) bool {
	return item.Tenant == tenant && item.Namespace == namespace
}
