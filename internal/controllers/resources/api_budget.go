// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

// ReplicationAPIConfigs reserves an independent API budget for namespace
// bootstrap. Background clients retain their existing configured limits and
// cannot consume bootstrap's tokens. All bootstrap identities share one bucket,
// bounding its added traffic independently of tenant count. Custom limiters and
// unlimited clients retain their caller-defined behavior.
func ReplicationAPIConfigs(base *rest.Config) (background, bootstrap *rest.Config) {
	background, bootstrap = rest.CopyConfig(base), rest.CopyConfig(base)
	if base.RateLimiter != nil || base.QPS < 0 {
		return background, bootstrap
	}

	qps, burst := base.QPS, base.Burst
	if qps == 0 {
		qps = rest.DefaultQPS
	}

	if burst <= 0 {
		burst = rest.DefaultBurst
	}

	bootstrap.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(qps, burst)

	return background, bootstrap
}
