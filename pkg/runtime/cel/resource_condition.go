// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cel

import "k8s.io/apiserver/pkg/cel/environment"

// ResourceConditionCompiler shares immutable compiled apply conditions between
// admission and controllers. Evaluation inputs and results are never cached.
type ResourceConditionCompiler interface {
	GetOrCompileResourceCondition(expression string, mode environment.Type) (*CompiledExpression, error)
}

func (c *Compiler) CompileResourceCondition(expression string, mode environment.Type) (*CompiledExpression, error) {
	return c.CompileBooleanWithVariables(expression, mode, "now")
}
