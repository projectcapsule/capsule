// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

func jitteredResync(period time.Duration) time.Duration {
	if period <= 0 {
		return 0
	}

	return wait.Jitter(period, 0.1)
}
