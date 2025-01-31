package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func IsNamespaceSelectedBySelector(ns *corev1.Namespace, selector *metav1.LabelSelector) (bool, error) {
	if selector == nil {
		return true, nil // If selector is nil, all namespaces match
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}

	return labelSelector.Matches(labels.Set(ns.Labels)), nil
}
