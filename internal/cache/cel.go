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
	expression string
	resultType celruntime.ResultType
	mode       environment.Type
}

type CELCache struct {
	mu       sync.RWMutex
	compiler *celruntime.Compiler
	data     map[celCacheKey]*celruntime.CompiledExpression
}

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

func (c *CELCache) GetOrCompileBoolean(
	expression string,
	mode environment.Type,
) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeBoolean, mode)
}

func (c *CELCache) GetOrCompileQuantity(
	expression string,
	mode environment.Type,
) (*celruntime.CompiledExpression, error) {
	return c.getOrCompile(expression, celruntime.ResultTypeQuantity, mode)
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

func (c *CELCache) getOrCompile(
	expression string,
	resultType celruntime.ResultType,
	mode environment.Type,
) (*celruntime.CompiledExpression, error) {
	if c == nil || c.compiler == nil {
		return nil, fmt.Errorf("CEL cache is nil")
	}

	key := celCacheKey{
		expression: expression,
		resultType: resultType,
		mode:       mode,
	}

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
		compiled, err = c.compiler.CompileBoolean(expression, mode)
	case celruntime.ResultTypeQuantity:
		compiled, err = c.compiler.CompileQuantity(expression, mode)
	default:
		err = fmt.Errorf("unsupported CEL result type %q", resultType)
	}

	if err != nil {
		return nil, err
	}

	c.data[key] = compiled

	return compiled, nil
}
