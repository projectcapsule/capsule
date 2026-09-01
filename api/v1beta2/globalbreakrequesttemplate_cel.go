// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

const (
	celRequestVariable   = "request"
	celRequestorVariable = "requestor"
	celReviewerVariable  = "reviewer"
	groupsField          = "groups"
)

// ValidateApprovalConditions compiles the template's approval conditions in
// the Kubernetes CEL environment used for newly submitted expressions.
func (brt *GlobalBreakRequestTemplate) ValidateApprovalConditions() error {
	return validateApprovalConditions(brt.Spec.Approvals)
}

// ValidateApprovalConditions compiles a namespaced template's approval conditions.
func (brt *BreakRequestTemplate) ValidateApprovalConditions() error {
	return validateApprovalConditions(brt.Spec.Approvals)
}

func validateApprovalConditions(approvals breaktheglass.ApprovalSpec) error {
	if len(approvals.Conditions) == 0 {
		return nil
	}

	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return err
	}

	for i, condition := range approvals.Conditions {
		if _, err := compiler.CompileBooleanWithVariables(
			condition,
			environment.NewExpressions,
			celRequestVariable,
			celRequestorVariable,
			celReviewerVariable,
		); err != nil {
			return fmt.Errorf("compile approval condition %d: %w", i, err)
		}
	}

	return nil
}

// EvaluateApprovalConditions evaluates the stored approval conditions against
// a BreakRequest and its requestor/reviewer identities. Conditions are ORed.
func (brt *GlobalBreakRequestTemplate) EvaluateApprovalConditions(
	ctx context.Context,
	br *BreakRequest,
) (bool, error) {
	return evaluateApprovalConditions(ctx, br, brt.Spec.Approvals)
}

// EvaluateApprovalConditions evaluates a namespaced template's approval conditions.
func (brt *BreakRequestTemplate) EvaluateApprovalConditions(
	ctx context.Context,
	br *BreakRequest,
) (bool, error) {
	return evaluateApprovalConditions(ctx, br, brt.Spec.Approvals)
}

func evaluateApprovalConditions(
	ctx context.Context,
	br *BreakRequest,
	approvals breaktheglass.ApprovalSpec,
) (bool, error) {
	if len(approvals.Conditions) == 0 {
		return true, nil
	}

	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return false, err
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

	variables := map[string]any{
		celRequestVariable:   request,
		celRequestorVariable: requestor,
		celReviewerVariable:  reviewer,
	}

	var evaluationErrors []error

	for i, condition := range approvals.Conditions {
		compiled, compileErr := compiler.CompileBooleanWithVariables(
			condition,
			environment.StoredExpressions,
			celRequestVariable,
			celRequestorVariable,
			celReviewerVariable,
		)
		if compileErr != nil {
			evaluationErrors = append(evaluationErrors, fmt.Errorf("compile approval condition %d: %w", i, compileErr))

			continue
		}

		matched, evaluationErr := compiled.EvaluateBooleanWithVariables(ctx, variables)
		if evaluationErr != nil {
			evaluationErrors = append(evaluationErrors, fmt.Errorf(
				"evaluate approval condition %d (%s): %w",
				i,
				condition,
				evaluationErr,
			))

			continue
		}

		if matched {
			return true, nil
		}
	}

	if len(evaluationErrors) > 0 {
		return false, errors.Join(evaluationErrors...)
	}

	return false, nil
}

// CheckApprovalConditions returns an error when the template's approval gate
// is invalid or none of its conditions match the supplied request.
func (brt *GlobalBreakRequestTemplate) CheckApprovalConditions(ctx context.Context, br *BreakRequest) error {
	return checkApprovalConditions(ctx, br, brt.Spec.Approvals)
}

// CheckApprovalConditions checks a namespaced template's approval gate.
func (brt *BreakRequestTemplate) CheckApprovalConditions(ctx context.Context, br *BreakRequest) error {
	return checkApprovalConditions(ctx, br, brt.Spec.Approvals)
}

func checkApprovalConditions(
	ctx context.Context,
	br *BreakRequest,
	approvals breaktheglass.ApprovalSpec,
) error {
	matched, err := evaluateApprovalConditions(ctx, br, approvals)
	if err != nil {
		return err
	}

	if !matched {
		return fmt.Errorf("none of the approval conditions were met")
	}

	return nil
}

func ensureGroups(entity map[string]any) {
	if _, exists := entity[groupsField]; !exists {
		entity[groupsField] = []string{}
	}
}
