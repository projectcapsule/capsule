// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

const (
	celRequestVariable   = "request"
	celRequestorVariable = "requestor"
	celReviewerVariable  = "reviewer"
	groupsField          = "groups"
)

// ValidateApprovalCondition compiles the template's approval condition in the
// Kubernetes CEL environment used for newly submitted expressions.
func (brt *BreakRequestTemplate) ValidateApprovalCondition() error {
	if brt.Spec.ApprovalCondition == "" {
		return nil
	}

	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return err
	}

	if _, err := compiler.CompileBooleanWithVariables(
		brt.Spec.ApprovalCondition,
		environment.NewExpressions,
		celRequestVariable,
		celRequestorVariable,
		celReviewerVariable,
	); err != nil {
		return fmt.Errorf("compile approval condition: %w", err)
	}

	return nil
}

// EvaluateApprovalCondition evaluates the stored approval condition against a
// BreakRequest and its requestor/reviewer identities.
func (brt *BreakRequestTemplate) EvaluateApprovalCondition(
	ctx context.Context,
	br *BreakRequest,
) (bool, error) {
	if brt.Spec.ApprovalCondition == "" {
		return true, nil
	}

	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return false, err
	}

	compiled, err := compiler.CompileBooleanWithVariables(
		brt.Spec.ApprovalCondition,
		environment.StoredExpressions,
		celRequestVariable,
		celRequestorVariable,
		celReviewerVariable,
	)
	if err != nil {
		return false, fmt.Errorf("compile approval condition: %w", err)
	}

	request, err := runtime.DefaultUnstructuredConverter.ToUnstructured(br)
	if err != nil {
		return false, fmt.Errorf("convert BreakRequest for approval condition: %w", err)
	}

	requestor, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&br.Spec.Requestor)
	if err != nil {
		return false, fmt.Errorf("convert requestor for approval condition: %w", err)
	}

	ensureGroups(requestor)

	reviewer := map[string]any{}
	if br.Status.Review != nil && br.Status.Review.Reviewer != nil {
		reviewer, err = runtime.DefaultUnstructuredConverter.ToUnstructured(br.Status.Review.Reviewer)
		if err != nil {
			return false, fmt.Errorf("convert reviewer for approval condition: %w", err)
		}
	}

	ensureGroups(reviewer)

	matched, err := compiled.EvaluateBooleanWithVariables(ctx, map[string]any{
		celRequestVariable:   request,
		celRequestorVariable: requestor,
		celReviewerVariable:  reviewer,
	})
	if err != nil {
		return false, fmt.Errorf(
			"evaluate approval condition (%s): %w",
			brt.Spec.ApprovalCondition,
			err,
		)
	}

	return matched, nil
}

// CheckApprovalCondition returns an error when the template's approval gate is
// invalid or does not match the supplied request.
func (brt *BreakRequestTemplate) CheckApprovalCondition(ctx context.Context, br *BreakRequest) error {
	matched, err := brt.EvaluateApprovalCondition(ctx, br)
	if err != nil {
		return err
	}

	if !matched {
		return fmt.Errorf("approval condition (%s) not met", brt.Spec.ApprovalCondition)
	}

	return nil
}

func ensureGroups(entity map[string]any) {
	if _, exists := entity[groupsField]; !exists {
		entity[groupsField] = []string{}
	}
}
