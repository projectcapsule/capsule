// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Attempt so verify multiple selector objects against a labels.Set
// If selectors are not set, it is always considered a match.
func MatchesSelectors(objLabels labels.Set, selectors []metav1.LabelSelector) bool {
	if len(selectors) == 0 {
		return true
	}

	for _, selector := range selectors {
		sel, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			continue
		}

		if sel.Matches(objLabels) {
			return true
		}
	}

	return false
}
