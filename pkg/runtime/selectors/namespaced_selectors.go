// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

// Selector for resources and their labels or selecting origin namespaces
// +kubebuilder:object:generate=true
type SelectorWithNamespaceSelector struct {
	// Select Items based on their labels. If the namespaceSelector is also set, the selector is applied
	// to items within the selected namespaces. Otherwise for all the items.
	*metav1.LabelSelector `json:",inline"`

	// NamespaceSelector for filtering namespaces by labels where items can be located in
	NamespaceSelector *NamespaceSelector `json:"namespaceSelector,omitempty"`
}

func (s *SelectorWithNamespaceSelector) MatchObjects(
	ctx context.Context,
	c client.Client,
	objects []metav1.Object,
) ([]metav1.Object, error) {
	if s == nil {
		return nil, nil
	}

	var objSelector labels.Selector

	if s.LabelSelector != nil {
		var err error

		objSelector, err = metav1.LabelSelectorAsSelector(s.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid namespace selector: %w", err)
		}
	}

	labelFilteredObjects := make([]metav1.Object, 0, len(objects))

	for _, obj := range objects {
		if objSelector != nil && !objSelector.Matches(labels.Set(obj.GetLabels())) {
			continue // Skip non-matching objects
		}

		labelFilteredObjects = append(labelFilteredObjects, obj)
	}

	if s.NamespaceSelector == nil {
		return labelFilteredObjects, nil
	}

	matchingNamespaces, err := s.NamespaceSelector.GetMatchingNamespaces(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("error fetching matching namespaces: %w", err)
	}

	namespaceSet := make(map[string]struct{})
	for _, ns := range matchingNamespaces {
		namespaceSet[ns.Name] = struct{}{}
	}

	finalMatchingObjects := make([]metav1.Object, 0, len(labelFilteredObjects))

	for _, obj := range labelFilteredObjects {
		if len(namespaceSet) > 0 {
			if _, exists := namespaceSet[obj.GetNamespace()]; !exists {
				continue // Skip objects in disallowed namespaces
			}
		}

		finalMatchingObjects = append(finalMatchingObjects, obj)
	}

	return finalMatchingObjects, nil
}
