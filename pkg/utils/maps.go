// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

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
