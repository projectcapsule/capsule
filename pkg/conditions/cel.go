// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const (
	celRequestVariable   = "request"
	celRequestorVariable = "requestor"
	celReviewerVariable  = "reviewer"
	groupsField          = "groups"
)

func IsAllowed(brt *capsulev1beta2.BreakRequestTemplate, br *capsulev1beta2.BreakRequest) error {
	if brt.Spec.ApprovalCondition == "" {
		return nil
	}

	prg, err := PrepareCondition(brt)
	if err != nil {
		return fmt.Errorf("failed to prepare approval condition: %w", err)
	}

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(br)
	if err != nil {
		return fmt.Errorf("failed to convert BreakRequest to unstructured: %w", err)
	}

	var reviewer any

	if br.Status.Review != nil && br.Status.Review.Reviewer != nil {
		reviewerMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(br.Status.Review.Reviewer)
		if _, ok := reviewerMap[groupsField]; !ok {
			reviewerMap[groupsField] = []string{}
		}

		reviewer = reviewerMap
	}

	requestorMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&br.Spec.Requestor)
	if err != nil {
		requestorMap = map[string]any{}
	}

	if _, ok := requestorMap[groupsField]; !ok {
		requestorMap[groupsField] = []string{}
	}

	result, _, err := prg.Eval(map[string]any{
		celRequestVariable:   obj,
		celRequestorVariable: requestorMap,
		celReviewerVariable:  reviewer,
	})
	if err != nil {
		return fmt.Errorf("runtime error evaluating approval condition (%s): %w", brt.Spec.ApprovalCondition, err)
	}

	// Convert the result to boolean
	boolResult, ok := result.Value().(bool)
	if !ok {
		return fmt.Errorf(
			"approval condition (%s) did not evaluate to a boolean, got %T",
			brt.Spec.ApprovalCondition,
			result.Value(),
		)
	}

	if !boolResult {
		return fmt.Errorf("approval condition (%s) not met", brt.Spec.ApprovalCondition)
	}

	return nil
}

func PrepareCondition(brt *capsulev1beta2.BreakRequestTemplate) (cel.Program, error) {
	env, err := cel.NewEnv(
		cel.Variable(celRequestVariable, cel.DynType),
		cel.Variable(celRequestorVariable, cel.DynType),
		cel.Variable(celReviewerVariable, cel.DynType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	ast, iss := env.Compile(brt.Spec.ApprovalCondition)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression: %w", iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}

	return prg, nil
}
