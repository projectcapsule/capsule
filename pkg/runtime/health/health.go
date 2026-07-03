// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package health evaluates the health of replicated objects, either via custom
// CEL expressions (mirroring Flux healthCheckExprs semantics) or, as a default,
// via kstatus. It is self-contained so that multiple controllers can reuse it.
package health

import (
	"fmt"

	"github.com/fluxcd/cli-utils/pkg/kstatus/status"
	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Status is the health classification of a single replicated object.
type Status string

const (
	// StatusHealthy indicates the object has reached its desired state.
	StatusHealthy Status = "Healthy"
	// StatusUnhealthy indicates the object has (permanently) failed.
	StatusUnhealthy Status = "Unhealthy"
	// StatusProgressing indicates the object is still reconciling, or its health
	// cannot yet be determined.
	StatusProgressing Status = "Progressing"
)

// Result is the outcome of evaluating a single object.
type Result struct {
	Status  Status
	Message string
}

// objectVariables are the top-level object fields exposed to CEL expressions,
// mirroring the Flux healthCheckExprs convention where fields are referenced
// directly (e.g. `status.conditions`). Referencing a field outside this set
// results in a compile error, surfaced by NewChecker and the webhook.
//
// Note: "type" is intentionally omitted because it collides with the CEL
// built-in `type` identifier; the rare Secret.type field is not exposed.
var objectVariables = []string{
	"apiVersion", "kind", "metadata", "spec", "status",
	"data", "stringData", "binaryData", "immutable",
}

// programs holds the compiled CEL programs for a single GVK.
type programs struct {
	current    cel.Program
	failed     cel.Program
	inProgress cel.Program
}

// Checker evaluates objects against a set of health checks. It is built once per
// reconcile via NewChecker and is safe for sequential reuse across objects.
type Checker struct {
	programs map[schema.GroupVersionKind]programs
}

// NewChecker compiles the CEL expressions of the given health checks. It returns
// an error if any apiVersion or expression is invalid, so problems surface once
// per reconcile rather than once per object.
func NewChecker(checks []capsulev1beta2.HealthCheckSpec) (*Checker, error) {
	env, err := newEnv()
	if err != nil {
		return nil, err
	}

	compiled := make(map[schema.GroupVersionKind]programs, len(checks))

	for _, hc := range checks {
		gv, err := schema.ParseGroupVersion(hc.APIVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid apiVersion %q: %w", hc.APIVersion, err)
		}

		gvk := gv.WithKind(hc.Kind)

		current, err := compile(env, hc.Current)
		if err != nil {
			return nil, fmt.Errorf("%s/%s current expression: %w", hc.APIVersion, hc.Kind, err)
		}

		failed, err := compile(env, hc.Failed)
		if err != nil {
			return nil, fmt.Errorf("%s/%s failed expression: %w", hc.APIVersion, hc.Kind, err)
		}

		inProgress, err := compile(env, hc.InProgress)
		if err != nil {
			return nil, fmt.Errorf("%s/%s inProgress expression: %w", hc.APIVersion, hc.Kind, err)
		}

		compiled[gvk] = programs{current: current, failed: failed, inProgress: inProgress}
	}

	return &Checker{programs: compiled}, nil
}

// Validate compiles the given health checks and additionally ensures every entry
// declares at least one of current/failed. It is intended for admission-time
// validation and reuses the exact CEL environment used at runtime.
func Validate(checks []capsulev1beta2.HealthCheckSpec) error {
	for i, hc := range checks {
		if hc.Current == "" && hc.Failed == "" {
			return fmt.Errorf("healthChecks[%d] (%s/%s): at least one of current or failed must be set", i, hc.APIVersion, hc.Kind)
		}
	}

	_, err := NewChecker(checks)

	return err
}

// Check evaluates a single live object. When a health check matches the object's
// GVK, the CEL expressions decide the outcome (failed -> unhealthy, else
// inProgress -> progressing, else current -> healthy, else progressing).
// Otherwise kstatus is used as the default.
func (c *Checker) Check(obj *unstructured.Unstructured) Result {
	if prg, ok := c.programs[obj.GroupVersionKind()]; ok {
		vars := obj.Object

		if evalBool(prg.failed, vars) {
			return Result{Status: StatusUnhealthy, Message: "failed health expression matched"}
		}

		// inProgress is evaluated before current so that an object which is still
		// settling is not prematurely reported healthy, even if current also matches.
		if evalBool(prg.inProgress, vars) {
			return Result{Status: StatusProgressing, Message: "inProgress health expression matched"}
		}

		if evalBool(prg.current, vars) {
			return Result{Status: StatusHealthy, Message: "current health expression matched"}
		}

		return Result{Status: StatusProgressing, Message: "waiting for current health expression to match"}
	}

	return kstatusResult(obj)
}

// kstatusResult classifies an object using kstatus, which understands the
// built-in workloads (Deployments, Jobs, ...) and the Ready-condition convention
// used by most custom resources.
func kstatusResult(obj *unstructured.Unstructured) Result {
	res, err := status.Compute(obj)
	if err != nil {
		return Result{Status: StatusProgressing, Message: err.Error()}
	}

	//nolint:exhaustive
	switch res.Status {
	case status.CurrentStatus:
		return Result{Status: StatusHealthy, Message: res.Message}
	case status.FailedStatus:
		return Result{Status: StatusUnhealthy, Message: res.Message}
	default:
		return Result{Status: StatusProgressing, Message: res.Message}
	}
}

// newEnv builds the CEL environment exposing the object's top-level fields.
func newEnv() (*cel.Env, error) {
	opts := make([]cel.EnvOption, 0, len(objectVariables))
	for _, v := range objectVariables {
		opts = append(opts, cel.Variable(v, cel.DynType))
	}

	return cel.NewEnv(opts...)
}

// compile compiles a single expression, returning a nil program for the empty
// expression. It rejects expressions that cannot evaluate to a boolean.
func compile(env *cel.Env, expr string) (cel.Program, error) {
	if expr == "" {
		return nil, nil
	}

	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}

	if out := ast.OutputType(); !out.IsExactType(cel.BoolType) && !out.IsExactType(cel.DynType) {
		return nil, fmt.Errorf("expression must evaluate to bool, got %s", out.String())
	}

	return env.Program(ast)
}

// evalBool evaluates a program against the object variables, treating a nil
// program, an evaluation error (e.g. a reference to an absent field), or a
// non-boolean result as "did not match".
func evalBool(prg cel.Program, vars map[string]any) bool {
	if prg == nil {
		return false
	}

	out, _, err := prg.Eval(vars)
	if err != nil {
		return false
	}

	b, ok := out.Value().(bool)

	return ok && b
}
