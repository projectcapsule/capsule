// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func MakeCustomQuotaCacheKey(namespace, name string) string {
	return namespace + "/" + name
}

func MakeGlobalCustomQuotaCacheKey(name string) string {
	return "C/" + name
}
func getResources(
	ctx context.Context,
	target *capsulev1beta2.CustomQuotaSpecSource,
	kubeClient client.Reader,
	scopeSelectors []metav1.LabelSelector,
	namespaces ...string,
) ([]unstructured.Unstructured, error) {
	compiledSelectors := make([]labels.Selector, 0, len(scopeSelectors))
	for _, selector := range scopeSelectors {
		sel, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return nil, err
		}
		compiledSelectors = append(compiledSelectors, sel)
	}

	namespaceSet := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		namespaceSet[ns] = struct{}{}
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind(target.GroupVersionKind))

	if err := kubeClient.List(ctx, list); err != nil {
		return nil, err
	}

	items := make([]unstructured.Unstructured, 0, len(list.Items))
	seen := make(map[string]struct{}, len(list.Items))

	for i := range list.Items {
		item := list.Items[i]

		// Skip objects that are already definitely deleting:
		// deletionTimestamp is set and there are no finalizers left.
		if item.GetDeletionTimestamp() != nil && len(item.GetFinalizers()) == 0 {
			continue
		}

		// Namespace filter
		if len(namespaceSet) > 0 {
			if _, ok := namespaceSet[item.GetNamespace()]; !ok {
				continue
			}
		}

		// Label selector filter (OR semantics)
		if len(compiledSelectors) > 0 {
			itemLabels := labels.Set(item.GetLabels())

			matched := false
			for _, sel := range compiledSelectors {
				if sel.Matches(itemLabels) {
					matched = true
					break
				}
			}

			if !matched {
				continue
			}
		}

		key := item.GetNamespace() + "/" + item.GetName()
		if item.GetNamespace() == "" {
			key = item.GetName()
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		items = append(items, item)
	}

	// Sort by oldest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetCreationTimestamp().Time.Before(items[j].GetCreationTimestamp().Time)
	})

	return items, nil
}
