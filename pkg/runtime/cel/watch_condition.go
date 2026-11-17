// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cel

import "k8s.io/apiserver/pkg/cel/environment"

// CompileWatchCondition deliberately excludes request identities and authorizer:
// a watch provides observed object states, not an admission request.
func (c *Compiler) CompileWatchCondition(expression string, mode environment.Type) (*CompiledExpression, error) {
	return c.compile(expression, mode, ResultTypeWatchCondition, []string{"oldObject", "operation"})
}
