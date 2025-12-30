// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Selector for resources and their labels or selecting origin namespaces
// +kubebuilder:object:generate=true
type NamespaceSelector struct {
	// Select Items based on their labels. If the namespaceSelector is also set, the selector is applied
	// to items within the selected namespaces. Otherwise for all the items.
	*metav1.LabelSelector `json:",inline"`
}

// GetMatchingNamespaces retrieves the list of namespaces that match the NamespaceSelector.
func (s *NamespaceSelector) GetMatchingNamespaces(
	ctx context.Context,
	c client.Client,
) ([]corev1.Namespace, error) {
	if s.LabelSelector == nil {
		return nil, nil // No namespace selector means all namespaces
	}

	nsSelector, err := metav1.LabelSelectorAsSelector(s.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace selector: %w", err)
	}

	namespaceList := &corev1.NamespaceList{}
	if err := c.List(ctx, namespaceList); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var matchingNamespaces []corev1.Namespace

	for _, ns := range namespaceList.Items {
		if nsSelector.Matches(labels.Set(ns.Labels)) {
			matchingNamespaces = append(matchingNamespaces, ns)
		}
	}

	return matchingNamespaces, nil
}

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
