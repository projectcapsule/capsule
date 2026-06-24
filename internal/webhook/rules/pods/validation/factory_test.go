// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import "github.com/projectcapsule/capsule/internal/cache"

func podRulesForTest() *podRules {
	return &podRules{
		regexCache: cache.NewRegexCache(),
	}
}
