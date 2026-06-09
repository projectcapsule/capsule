// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func IsApproved(brt *capsulev1beta2.BreakRequestTemplate, br *capsulev1beta2.BreakRequest) (bool, error) {
	if !brt.Spec.AutoApprove {
		return false, nil
	}

	if brt.Spec.ApprovalCondition == "" {
		return true, nil
	}

	prg, err := PrepareCondition(brt)
	if err != nil {
		return false, err
	}

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(br)
	if err != nil {
		return false, err
	}

	result, _, err := prg.Eval(map[string]any{
		"request": obj,
	})
	if err != nil {
		return false, err
	}

	// Convert the result to boolean
	boolResult, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf(
			"expression did not evaluate to a boolean, got: %T",
			result.Value(),
		)
	}

	return boolResult, nil
}

func PrepareCondition(brt *capsulev1beta2.BreakRequestTemplate) (cel.Program, error) {
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
	)
	if err != nil {
		return nil, err
	}

	ast, iss := env.Compile(brt.Spec.ApprovalCondition)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}

	return env.Program(ast)
}
