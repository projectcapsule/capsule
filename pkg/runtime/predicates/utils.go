// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

func LabelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}

	return true
}

func LabelsChanged(keys []string, oldLabels, newLabels map[string]string) bool {
	for _, key := range keys {
		oldVal, oldOK := oldLabels[key]
		newVal, newOK := newLabels[key]

		if oldOK != newOK || oldVal != newVal {
			return true
		}
	}

	return false
}
