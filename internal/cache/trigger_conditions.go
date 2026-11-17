// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"

	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

const maxTriggerConditions = 1024

// GetOrCompileTriggerCondition reuses immutable object-state programs. Prepared
// subscriptions retain their programs independently of this bounded compile cache.
func (c *CELCache) GetOrCompileTriggerCondition(expression string) (*celruntime.CompiledExpression, error) {
	if c == nil || c.compiler == nil {
		return nil, fmt.Errorf("CEL cache is nil")
	}

	c.triggerMu.RLock()
	compiled := c.triggerConditions[expression]
	c.triggerMu.RUnlock()

	if compiled != nil {
		return compiled, nil
	}

	compiled, err := c.compiler.CompileWatchCondition(expression, environment.NewExpressions)
	if err != nil {
		return nil, err
	}

	c.triggerMu.Lock()
	defer c.triggerMu.Unlock()

	if existing := c.triggerConditions[expression]; existing != nil {
		return existing, nil
	}

	if c.triggerConditions == nil {
		c.triggerConditions = make(map[string]*celruntime.CompiledExpression)
	}

	if len(c.triggerOrder) == maxTriggerConditions {
		delete(c.triggerConditions, c.triggerOrder[c.triggerNext])
		c.triggerOrder[c.triggerNext] = expression
		c.triggerNext = (c.triggerNext + 1) % maxTriggerConditions
	} else {
		c.triggerOrder = append(c.triggerOrder, expression)
	}

	c.triggerConditions[expression] = compiled

	return compiled, nil
}

func (c *CELCache) ValidateTriggerCondition(expression string) error {
	_, err := c.GetOrCompileTriggerCondition(expression)

	return err
}
