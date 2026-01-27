// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ShouldInvalidate(last *metav1.Time, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return false
	}

	if last == nil || last.IsZero() {
		return true
	}

	if last.After(now) {
		return false
	}

	return now.Sub(last.Time) >= interval
}
