// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import "k8s.io/apimachinery/pkg/runtime"

func MapMergeNoOverrite(dst, src map[string]string) {
	if len(src) == 0 {
		return
	}

	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func MapEqual(a, b map[string]string) bool {
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

func ToMap(obj any) (map[string]any, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return m, nil
}
