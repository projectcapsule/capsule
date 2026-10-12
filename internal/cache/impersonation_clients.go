// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import runtimecache "github.com/projectcapsule/capsule/pkg/runtime/cache"

type (
	Key                = runtimecache.Key
	ImpersonationCache = runtimecache.ImpersonationCache
)

var NewImpersonationCache = runtimecache.NewImpersonationCache
