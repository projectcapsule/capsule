// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListBySelectors lists objects of type T (using list L), then returns all items that
// match ANY of the provided LabelSelectors. The result is unique by namespace/name.
func ListBySelectors[T client.Object](
	ctx context.Context,
	c client.Client,
	list client.ObjectList,
	selectors []*metav1.LabelSelector,
) ([]T, error) {
	if len(selectors) == 0 {
		return nil, nil
	}

	if list == nil {
		return nil, fmt.Errorf("list must not be nil")
	}

	// Preallocate with upper bound (len(selectors)); nil selectors will just not be used.
	selList := make([]labels.Selector, 0, len(selectors))

	for _, ls := range selectors {
		if ls == nil {
			continue
		}

		sel, err := metav1.LabelSelectorAsSelector(ls)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector %v: %w", ls, err)
		}

		selList = append(selList, sel)
	}

	if len(selList) == 0 {
		return nil, nil
	}

	// List all objects once
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("listing objects: %w", err)
	}

	rawItems, err := meta.ExtractList(list)
	if err != nil {
		return nil, fmt.Errorf("extracting list items: %w", err)
	}

	// Deduplicate by namespace/name
	seen := make(map[client.ObjectKey]struct{}, len(rawItems))

	// Upper bound: at most len(rawItems) will match; good enough for prealloc.
	result := make([]T, 0, len(rawItems))

	for _, obj := range rawItems {
		typed, ok := obj.(T)
		if !ok {
			continue
		}

		lbls := typed.GetLabels()
		if len(lbls) == 0 {
			continue
		}

		set := labels.Set(lbls)

		// Match against ANY selector
		matched := false

		for _, sel := range selList {
			if sel.Matches(set) {
				matched = true

				break
			}
		}

		if !matched {
			continue
		}

		key := client.ObjectKeyFromObject(typed)
		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}

		result = append(result, typed)
	}

	return result, nil
}
