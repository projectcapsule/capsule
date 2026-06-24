// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type ControllerOptions struct {
	ConfigurationName string
	Runtime           RuntimeControllerOptions
}

type RuntimeControllerOptions struct {
	MaxConcurrentReconciles int
	CacheSyncTimeout        time.Duration
}

func (o RuntimeControllerOptions) ToControllerOptions() controller.Options {
	out := controller.Options{
		MaxConcurrentReconciles: o.MaxConcurrentReconciles,
	}

	if o.CacheSyncTimeout > 0 {
		out.CacheSyncTimeout = o.CacheSyncTimeout
	}

	return out
}
