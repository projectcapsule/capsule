// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cel

import (
	"context"
	"fmt"
	"strings"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/version"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
)

const (
	ObjectVariable      = "object"
	MaxExpressionLength = 4096
)

type ResultType string

const (
	ResultTypeBoolean  ResultType = "boolean"
	ResultTypeQuantity ResultType = "quantity"
)

type Compiler struct {
	envSet *environment.EnvSet
}

type CompiledExpression struct {
	expression string
	program    celgo.Program
	resultType ResultType
}

func NewCompiler() (*Compiler, error) {
	base := environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion())

	envSet, err := base.Extend(
		environment.VersionedOptions{
			IntroducedVersion: version.MajorMinor(1, 0),
			EnvOptions: []celgo.EnvOption{
				celgo.Variable(ObjectVariable, celgo.DynType),
			},
		},
		environment.StrictCostOpt,
	)
	if err != nil {
		return nil, fmt.Errorf("build Kubernetes CEL environment: %w", err)
	}

	return &Compiler{envSet: envSet}, nil
}

func (c *Compiler) CompileBoolean(expression string, mode environment.Type) (*CompiledExpression, error) {
	return c.compile(expression, mode, ResultTypeBoolean)
}

func (c *Compiler) CompileQuantity(expression string, mode environment.Type) (*CompiledExpression, error) {
	return c.compile(expression, mode, ResultTypeQuantity)
}

func (c *Compiler) compile(
	expression string,
	mode environment.Type,
	resultType ResultType,
) (*CompiledExpression, error) {
	if c == nil || c.envSet == nil {
		return nil, fmt.Errorf("CEL compiler is nil")
	}

	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("CEL expression must not be empty")
	}

	if len(expression) > MaxExpressionLength {
		return nil, fmt.Errorf("CEL expression exceeds max length of %d", MaxExpressionLength)
	}

	env, err := c.envSet.Env(mode)
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes CEL %s environment: %w", mode, err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile CEL expression %q: %w", expression, issues.Err())
	}

	if err := validateOutputType(ast.OutputType(), resultType); err != nil {
		return nil, fmt.Errorf("compile CEL expression %q: %w", expression, err)
	}

	program, err := env.Program(
		ast,
		celgo.InterruptCheckFrequency(celconfig.CheckFrequency),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL program for %q: %w", expression, err)
	}

	return &CompiledExpression{
		expression: expression,
		program:    program,
		resultType: resultType,
	}, nil
}

func validateOutputType(output *celgo.Type, resultType ResultType) error {
	switch resultType {
	case ResultTypeBoolean:
		if !output.IsExactType(celgo.BoolType) {
			return fmt.Errorf("expression must evaluate to bool, got %s", output)
		}

	case ResultTypeQuantity:
		quantityListType := celgo.ListType(apiservercel.QuantityType)
		if !output.IsExactType(apiservercel.QuantityType) && !output.IsExactType(quantityListType) {
			return fmt.Errorf(
				"expression must evaluate to kubernetes.Quantity or list<kubernetes.Quantity>, got %s",
				output,
			)
		}

	default:
		return fmt.Errorf("unsupported CEL result type %q", resultType)
	}

	return nil
}

func (c *CompiledExpression) Expression() string {
	if c == nil {
		return ""
	}

	return c.expression
}

func (c *CompiledExpression) EvaluateBoolean(
	ctx context.Context,
	object unstructured.Unstructured,
) (bool, error) {
	if c == nil || c.program == nil {
		return false, fmt.Errorf("compiled CEL expression is nil")
	}

	if c.resultType != ResultTypeBoolean {
		return false, fmt.Errorf("compiled CEL expression %q does not return bool", c.expression)
	}

	value, _, err := c.program.ContextEval(ctx, map[string]any{
		ObjectVariable: object.Object,
	})
	if err != nil {
		return false, fmt.Errorf("evaluate CEL expression %q: %w", c.expression, err)
	}

	result, ok := value.(types.Bool)
	if !ok {
		return false, fmt.Errorf("CEL expression %q returned %T, expected bool", c.expression, value)
	}

	return bool(result), nil
}

func (c *CompiledExpression) EvaluateQuantity(
	ctx context.Context,
	object unstructured.Unstructured,
) (resource.Quantity, error) {
	if c == nil || c.program == nil {
		return resource.Quantity{}, fmt.Errorf("compiled CEL expression is nil")
	}

	if c.resultType != ResultTypeQuantity {
		return resource.Quantity{}, fmt.Errorf(
			"compiled CEL expression %q does not return a quantity",
			c.expression,
		)
	}

	value, _, err := c.program.ContextEval(ctx, map[string]any{
		ObjectVariable: object.Object,
	})
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("evaluate CEL expression %q: %w", c.expression, err)
	}

	if quantity, ok := value.(apiservercel.Quantity); ok {
		if quantity.Quantity == nil {
			return resource.Quantity{}, fmt.Errorf("CEL expression %q returned a nil quantity", c.expression)
		}

		return quantity.DeepCopy(), nil
	}

	list, ok := value.(traits.Lister)
	if !ok {
		return resource.Quantity{}, fmt.Errorf(
			"CEL expression %q returned %T, expected kubernetes.Quantity or list<kubernetes.Quantity>",
			c.expression,
			value,
		)
	}

	total := resource.Quantity{}
	count := 0
	iterator := list.Iterator()

	for iterator.HasNext() == types.True {
		item := iterator.Next()

		quantity, ok := item.(apiservercel.Quantity)
		if !ok || quantity.Quantity == nil {
			return resource.Quantity{}, fmt.Errorf(
				"CEL expression %q returned a list containing %T, expected kubernetes.Quantity",
				c.expression,
				item,
			)
		}

		total.Add(quantity.DeepCopy())

		count++
	}

	if count == 0 {
		return resource.Quantity{}, fmt.Errorf("CEL expression %q returned an empty quantity list", c.expression)
	}

	return total, nil
}
