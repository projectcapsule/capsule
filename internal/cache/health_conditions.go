// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

// GetOrCompileHealthCondition reuses the bounded lifecycle-expression cache.
// Its distinct key retains health bindings without exposing them to other CEL.
func (c *CELCache) GetOrCompileHealthCondition(expression string) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeHealthCondition, environment.NewExpressions, true)
}
