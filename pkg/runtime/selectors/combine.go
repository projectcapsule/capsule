// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import "k8s.io/apimachinery/pkg/labels"

func CombineSelectors(selectors ...labels.Selector) labels.Selector {
	combined := labels.NewSelector()

	for _, sel := range selectors {
		if sel == nil {
			continue
		}

		reqs, selectable := sel.Requirements()
		if !selectable {
			return labels.Nothing()
		}

		for _, r := range reqs {
			combined = combined.Add(r)
		}
	}

	return combined
}
