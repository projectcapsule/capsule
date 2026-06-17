// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"fmt"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

type Value struct {
	Value string
	Path  string
}

type Set[R any, T any] struct {
	Name string

	// EventReason is the Kubernetes event reason used by the admission layer
	// when this rule set produces in deny decisions.
	EventReason string

	// Values extracts the values from the admitted object.
	Values func(T) []Value

	// Rules extracts rule entries from an enforce body.
	Rules func(*api.NamespaceRuleEnforceBody) []R

	// Matches checks whether a rule entry matches an extracted value.
	Matches func(R, Value) (bool, error)
}

type Decision struct {
	Action api.ActionType

	// SetName is the name of the evaluated rule set, e.g. "scheduler", "QoS class".
	SetName string

	// EventReason is the Kubernetes event reason used by the admission layer
	// when this rule set produces in deny decisions.
	EventReason string

	// Value is the object value that matched or violated the rule set.
	Value Value

	// Enforce is the enforce body that produced the decision.
	Enforce *api.NamespaceRuleEnforceBody

	// Rule is the concrete matched rule item.
	//
	// This is intentionally any because Evaluation is non-generic and can be
	// consumed by admission handlers without knowing the rule item type.
	Rule any

	// Message is the human-readable decision message.
	Message string
}

type Evaluation struct {
	// Decision is the blocking result.
	//
	// It is set for:
	//   - explicit deny match
	//   - allow-list miss
	Decision *Decision

	// Audits contains all matched audit rules.
	//
	// Audit decisions are non-blocking and should be converted to Kubernetes
	// events / admission warnings by the admission handler.
	Audits []*Decision
}

func (e *Evaluation) BlockingError() error {
	if e == nil || e.Decision == nil {
		return nil
	}

	return &DecisionError{
		Decision: e.Decision,
	}
}

type DecisionError struct {
	Decision *Decision
}

func (e *DecisionError) Error() string {
	if e == nil || e.Decision == nil {
		return ""
	}

	return e.Decision.Message
}

// EvaluateEnforce evaluates one rule set against a list of enforce bodies.
//
// Semantics:
//   - deny is immediately blocking on match
//   - audit is non-blocking on match
//   - allow behaves as an allow-list only when at least one allow rule exists
//     for the current rule set
//   - if allow rules exist and none matches the value, the value is rejected
func EvaluateEnforce[R any, T any](
	obj T,
	enforceBodies []*api.NamespaceRuleEnforceBody,
	set Set[R, T],
) (*Evaluation, error) {
	if set.Name == "" {
		return nil, fmt.Errorf("rule set name is empty")
	}

	if set.Values == nil {
		return nil, fmt.Errorf("%s: values extractor is nil", set.Name)
	}

	if set.Rules == nil {
		return nil, fmt.Errorf("%s: rules extractor is nil", set.Name)
	}

	if set.Matches == nil {
		return nil, fmt.Errorf("%s: matcher is nil", set.Name)
	}

	evaluation := &Evaluation{}

	values := set.Values(obj)
	if len(values) == 0 {
		return evaluation, nil
	}

	for _, value := range values {
		if value.Value == "" {
			continue
		}

		hasAllowRule := false
		allowedByRule := false

		for _, enforce := range enforceBodies {
			if enforce == nil {
				continue
			}

			items := set.Rules(enforce)
			if len(items) == 0 {
				continue
			}

			action := enforce.Action.OrDefault()

			switch action {
			case api.ActionTypeAllow:
				hasAllowRule = true

			case api.ActionTypeDeny, api.ActionTypeAudit:
				// Supported actions.

			default:
				return nil, fmt.Errorf(
					"%s: unsupported rule action %q",
					set.Name,
					action,
				)
			}

			for _, item := range items {
				matched, err := set.Matches(item, value)
				if err != nil {
					return nil, fmt.Errorf("%s: invalid rule: %w", set.Name, err)
				}

				if !matched {
					continue
				}

				switch action {
				case api.ActionTypeDeny:
					evaluation.Decision = &Decision{
						Action:      action,
						SetName:     set.Name,
						EventReason: set.EventReason,
						Value:       value,
						Enforce:     enforce,
						Rule:        item,
						Message: fmt.Sprintf(
							"%s %q at %s is denied by tenant rule",
							set.Name,
							value.Value,
							value.Path,
						),
					}

					return evaluation, nil

				case api.ActionTypeAllow:
					allowedByRule = true

				case api.ActionTypeAudit:
					evaluation.Audits = append(evaluation.Audits, &Decision{
						Action:      action,
						SetName:     set.Name,
						EventReason: set.EventReason,
						Value:       value,
						Enforce:     enforce,
						Rule:        item,
						Message: fmt.Sprintf(
							"%s %q at %s matched audit tenant rule",
							set.Name,
							value.Value,
							value.Path,
						),
					})
				}
			}
		}

		if hasAllowRule && !allowedByRule {
			evaluation.Decision = &Decision{
				Action:      api.ActionTypeDeny,
				SetName:     set.Name,
				EventReason: set.EventReason,
				Value:       value,
				Message: fmt.Sprintf(
					"%s %q at %s is not allowed by tenant rule",
					set.Name,
					value.Value,
					value.Path,
				),
			}

			return evaluation, nil
		}
	}

	return evaluation, nil
}
