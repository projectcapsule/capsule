// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"

	"github.com/google/cel-go/cel"
	admissioncel "k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/cel/environment"
)

type triggerExpression string

// Keep the published working set warm: 128 native groups x 60 conditions,
// with headroom for built-in predicates. Only successful validation is
// retained: execution belongs to the API server, so keeping its programs here
// would unnecessarily retain thousands of compiled artifacts.
const maxTriggerConditions = 8192

func (e triggerExpression) GetExpression() string    { return string(e) }
func (e triggerExpression) ReturnTypes() []*cel.Type { return []*cel.Type{cel.BoolType} }

// ValidateTriggerCondition reuses bounded native admission compilations. Native
// CEL declarations deliberately include authorizer, unlike rule condition CEL.
func (c *CELCache) ValidateTriggerCondition(expression string) error {
	if c == nil {
		return fmt.Errorf("CEL cache is nil")
	}

	c.triggerMu.RLock()
	_, hit := c.triggerConditions[expression]
	compiler := c.triggerCompiler
	c.triggerMu.RUnlock()

	if hit {
		return nil
	}

	if compiler == nil {
		c.triggerMu.Lock()
		if c.triggerCompiler == nil {
			c.triggerCompiler = admissioncel.NewCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
		}

		compiler = c.triggerCompiler
		c.triggerMu.Unlock()
	}

	// Native compilation must not block warm quota/rule CEL lookups. Build
	// immutable artifacts outside publication locks, as in the selector cache.
	compiled := compiler.CompileCELExpression(triggerExpression(expression), admissioncel.OptionalVariableDeclarations{HasAuthorizer: true}, environment.NewExpressions)
	if compiled.Error != nil {
		return fmt.Errorf("invalid admission match condition: %w", compiled.Error)
	}

	c.triggerMu.Lock()
	defer c.triggerMu.Unlock()

	if _, exists := c.triggerConditions[expression]; exists {
		return nil
	}

	if c.triggerConditions == nil {
		c.triggerConditions = make(map[string]struct{})
	}

	if len(c.triggerOrder) == maxTriggerConditions {
		delete(c.triggerConditions, c.triggerOrder[c.triggerNext])
		c.triggerOrder[c.triggerNext] = expression
		c.triggerNext = (c.triggerNext + 1) % maxTriggerConditions
	} else {
		c.triggerOrder = append(c.triggerOrder, expression)
	}

	c.triggerConditions[expression] = struct{}{}

	return nil
}
