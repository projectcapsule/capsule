// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apiserver/pkg/cel/environment"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

// ConditionCompiler is backed by the controller's shared CEL cache.
type ConditionCompiler interface {
	GetOrCompileCondition(expression string, mode environment.Type) (*celruntime.CompiledExpression, error)
}

// ConditionEvaluator owns request-local metadata, never evaluation results.
type ConditionEvaluator struct {
	compiler ConditionCompiler
	request  admissionv1.AdmissionRequest
	metadata map[string]any
	object   map[string]any
}

func NewConditionEvaluator(compiler ConditionCompiler, request admissionv1.AdmissionRequest) *ConditionEvaluator {
	return &ConditionEvaluator{compiler: compiler, request: request}
}

// ResetObject invalidates only the current request's object view after mutation.
func (e *ConditionEvaluator) ResetObject() {
	if e != nil {
		e.object = nil
	}
}

// Matches evaluates a block against its current object. A false condition takes
// precedence over errors, independently of condition order. A nil object uses
// the full admission request object, for callers that decode only metadata.
func (e *ConditionEvaluator) Matches(ctx context.Context, object any, conditions []rules.AdmissionCondition) (bool, error) {
	if len(conditions) == 0 {
		return true, nil
	}

	if e == nil || e.compiler == nil {
		return false, fmt.Errorf("admission condition compiler is unavailable")
	}

	if e.metadata == nil {
		request := e.request
		request.Object = runtime.RawExtension{}
		request.OldObject = runtime.RawExtension{}
		request.Options = runtime.RawExtension{}

		metadata, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&request)
		if err != nil {
			return false, fmt.Errorf("decode condition request metadata: %w", err)
		}

		delete(metadata, "object")
		delete(metadata, "oldObject")
		delete(metadata, "options")
		e.metadata = metadata
	}

	if e.object == nil {
		values, err := e.decodeObject(object)
		if err != nil {
			return false, fmt.Errorf("decode condition object: %w", err)
		}

		e.object = values
	}

	if e.object == nil {
		return false, fmt.Errorf("condition object must be an object")
	}

	var firstError error

	for i, condition := range conditions {
		compiled, compileErr := e.compiler.GetOrCompileCondition(condition.Expression, environment.StoredExpressions)

		matched := false

		if compileErr == nil {
			matched, compileErr = compiled.EvaluateCondition(ctx, e.object, e.metadata)
		}

		if compileErr == nil && !matched {
			return false, nil
		}

		if compileErr != nil && firstError == nil {
			firstError = fmt.Errorf("conditions[%d] (%q): %w", i, condition.Name, compileErr)
		}
	}

	return firstError == nil, firstError
}

func (e *ConditionEvaluator) decodeObject(object any) (map[string]any, error) {
	if object != nil {
		return runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	}

	var values map[string]any
	if err := json.Unmarshal(e.request.Object.Raw, &values); err != nil {
		return nil, err
	}

	return values, nil
}

// FilterEnforcementConditions filters the applicable enforcement rules. It
// preserves rule order and cache-owned bodies, with no allocation when ungated.
func FilterEnforcementConditions(
	ctx context.Context,
	evaluator *ConditionEvaluator,
	object any,
	bodies []*rules.NamespaceRuleEnforceBody,
	applies func(*rules.NamespaceRuleEnforceBody) bool,
) ([]*rules.NamespaceRuleEnforceBody, error) {
	var filtered []*rules.NamespaceRuleEnforceBody

	for i, body := range bodies {
		if body == nil {
			continue
		}

		var conditions []rules.AdmissionCondition
		if len(body.Conditions) > 0 && applies(body) {
			conditions = body.Conditions
		}

		matched, err := evaluator.Matches(ctx, object, conditions)
		if err != nil {
			return nil, fmt.Errorf("enforcement rule[%d]: %w", i, err)
		}

		if !matched && filtered == nil {
			filtered = make([]*rules.NamespaceRuleEnforceBody, 0, len(bodies))
			filtered = append(filtered, bodies[:i]...)
		}

		if matched && filtered != nil {
			filtered = append(filtered, body)
		}
	}

	if filtered == nil {
		return bodies, nil
	}

	return filtered, nil
}

// FilterNamespaceEnforcementConditions gates enforcement-derived mutations while
// retaining independent mutate entries. It copies only bodies whose gate is false.
func FilterNamespaceEnforcementConditions(
	ctx context.Context,
	evaluator *ConditionEvaluator,
	object any,
	bodies []*rules.NamespaceRuleBodyNamespace,
	applies func(*rules.NamespaceRuleEnforceBody) bool,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	var filtered []*rules.NamespaceRuleBodyNamespace

	for i, body := range bodies {
		if body == nil || body.Enforce == nil || len(body.Enforce.Conditions) == 0 || !applies(body.Enforce) {
			continue
		}

		matched, err := evaluator.Matches(ctx, object, body.Enforce.Conditions)
		if err != nil {
			return nil, fmt.Errorf("rules[%d].enforce: %w", i, err)
		}

		if !matched {
			if filtered == nil {
				filtered = append([]*rules.NamespaceRuleBodyNamespace(nil), bodies...)
			}

			filteredBody := *body
			filteredBody.Enforce = nil
			filtered[i] = &filteredBody
		}
	}

	if filtered == nil {
		return bodies, nil
	}

	return filtered, nil
}
