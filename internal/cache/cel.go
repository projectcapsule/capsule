// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"

	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

type celCacheKey struct {
	expression        string
	resultType        celruntime.ResultType
	mode              environment.Type
	resourceCondition bool
}

type CELCache struct {
	mu                 sync.RWMutex
	compiler           *celruntime.Compiler
	data               map[celCacheKey]*celruntime.CompiledExpression
	resourceConditions int
}

// Resource policies can be edited independently of the quota invalidation
// lifecycle. Bound their compiled working set within the shared cache.
const maxResourceConditions = 256

func NewCELCache() (*CELCache, error) {
	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return nil, err
	}

	return &CELCache{
		compiler: compiler,
		data:     make(map[celCacheKey]*celruntime.CompiledExpression),
	}, nil
}

func (c *CELCache) GetOrCompileResourceCondition(expression string, mode environment.Type) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeBoolean, mode, true)
}

func (c *CELCache) GetOrCompileBoolean(
	expression string,
	mode environment.Type,
) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeBoolean, mode, false)
}

func (c *CELCache) GetOrCompileQuantity(
	expression string,
	mode environment.Type,
) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeQuantity, mode, false)
}

func (c *CELCache) DeleteMany(expressions ...string) int {
	if c == nil {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	deleted := 0

	for key := range c.data {
		for _, expression := range expressions {
			if expression != "" && key.expression == expression {
				delete(c.data, key)

				if key.resourceCondition {
					c.resourceConditions--
				}

				deleted++

				break
			}
		}
	}

	return deleted
}

func (c *CELCache) Stats() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// Reset retires cached programs without changing programs already in use.
func (c *CELCache) Reset() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[celCacheKey]*celruntime.CompiledExpression)
	c.resourceConditions = 0
}

func (c *CELCache) getOrCompile(
	expression string,
	resultType celruntime.ResultType,
	mode environment.Type,
	resourceCondition bool,
) (*celruntime.CompiledExpression, error) {
	if c == nil || c.compiler == nil {
		return nil, fmt.Errorf("CEL cache is nil")
	}

	key := celCacheKey{
		expression: expression,
		resultType: resultType,
		mode:       mode,
	}
	key.resourceCondition = resourceCondition

	c.mu.RLock()
	compiled, ok := c.data[key]
	c.mu.RUnlock()

	if ok {
		return compiled, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if compiled, ok = c.data[key]; ok {
		return compiled, nil
	}

	var err error

	//nolint:exhaustive //cel.ResultTypeString not used yet
	switch resultType {
	case celruntime.ResultTypeBoolean:
		if key.resourceCondition {
			compiled, err = c.compiler.CompileResourceCondition(expression, mode)
		} else {
			compiled, err = c.compiler.CompileBoolean(expression, mode)
		}
	case celruntime.ResultTypeQuantity:
		compiled, err = c.compiler.CompileQuantity(expression, mode)
	default:
		err = fmt.Errorf("unsupported CEL result type %q", resultType)
	}

	if err != nil {
		return nil, err
	}

	if key.resourceCondition {
		if c.resourceConditions >= maxResourceConditions {
			for oldKey := range c.data {
				if oldKey.resourceCondition {
					delete(c.data, oldKey)

					c.resourceConditions--

					break
				}
			}
		}

		c.resourceConditions++
	}

	c.data[key] = compiled

	return compiled, nil
}
