// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cel

import "k8s.io/apiserver/pkg/cel/environment"

// CompileHealthCondition uses the bounded object-state evaluator, with Flux-style
// top-level fields and Capsule's object binding. No admission identity is exposed.
func (c *Compiler) CompileHealthCondition(expression string, mode environment.Type) (*CompiledExpression, error) {
	return c.compile(expression, mode, ResultTypeHealthCondition, []string{"apiVersion", "kind", "metadata", "spec", "status"})
}
