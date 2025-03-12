package api

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
func (s *NamespaceSelector) GetMatchingNamespaces(ctx context.Context, client client.Client) ([]corev1.Namespace, error) {
	if s.LabelSelector == nil {
		return nil, nil // No namespace selector means all namespaces
	}

	nsSelector, err := metav1.LabelSelectorAsSelector(s.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace selector: %w", err)
	}

	namespaceList := &corev1.NamespaceList{}
	if err := client.List(context.TODO(), namespaceList); err != nil {
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
