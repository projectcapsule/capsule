package rules

import (
	"fmt"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

type Value struct {
	Value string
	Path  string
}

type Decision struct {
	SetName     string
	EventReason string
	Action      api.ActionType
	Value       Value
	Message     string
}

type DecisionError struct {
	Decision *Decision
}

func (e *DecisionError) Error() string {
	if e == nil || e.Decision == nil {
		return "namespace rule decision denied request"
	}

	return e.Decision.Message
}

type Evaluation struct {
	Audits   []*Decision
	Blocking *Decision
}

func (e *Evaluation) BlockingError() error {
	if e == nil || e.Blocking == nil {
		return nil
	}

	return &DecisionError{
		Decision: e.Blocking,
	}
}

type Set[R any, T any] struct {
	Name string

	// EventReason is used when a matching rule produces an audit or blocking decision.
	EventReason string

	// Values extracts the values from the admitted object.
	Values func(T) []Value

	// Rules extracts rule entries from an enforce body.
	Rules func(*api.NamespaceRuleEnforceBody) []R

	// Matches checks whether a rule entry matches an extracted value.
	Matches func(R, Value) (bool, error)
}

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

		var lastDecision *Decision

		for _, enforce := range enforceBodies {
			if enforce == nil {
				continue
			}

			items := set.Rules(enforce)
			if len(items) == 0 {
				continue
			}

			switch enforce.Action {
			case api.ActionTypeAllow:
				hasAllowRule = true

			case api.ActionTypeDeny, api.ActionTypeAudit:
				// Supported actions.

			default:
				return evaluation, fmt.Errorf(
					"%s: unsupported rule action %q",
					set.Name,
					enforce.Action,
				)
			}

			for _, item := range items {
				matched, err := set.Matches(item, value)
				if err != nil {
					return evaluation, fmt.Errorf("%s: invalid rule: %w", set.Name, err)
				}

				if !matched {
					continue
				}

				decision := &Decision{
					SetName:     set.Name,
					EventReason: set.EventReason,
					Action:      enforce.Action,
					Value:       value,
					Message:     decisionMessage(set.Name, enforce.Action, value),
				}

				switch enforce.Action {
				case api.ActionTypeAudit:
					evaluation.Audits = append(evaluation.Audits, decision)

				case api.ActionTypeAllow, api.ActionTypeDeny:
					// Last matching allow/deny wins.
					lastDecision = decision
				}
			}
		}

		if lastDecision != nil {
			switch lastDecision.Action {
			case api.ActionTypeAllow:
				continue

			case api.ActionTypeDeny:
				evaluation.Blocking = lastDecision
				return evaluation, nil
			}
		}

		if hasAllowRule {
			evaluation.Blocking = &Decision{
				SetName:     set.Name,
				EventReason: set.EventReason,
				Action:      api.ActionTypeDeny,
				Value:       value,
				Message: fmt.Sprintf(
					"%s %q at %s is not allowed by namespace rule",
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

func decisionMessage(
	setName string,
	action api.ActionType,
	value Value,
) string {
	switch action {
	case api.ActionTypeAudit:
		return fmt.Sprintf(
			"%s %q at %s matched audit namespace rule",
			setName,
			value.Value,
			value.Path,
		)

	case api.ActionTypeDeny:
		return fmt.Sprintf(
			"%s %q at %s is denied by namespace rule",
			setName,
			value.Value,
			value.Path,
		)

	case api.ActionTypeAllow:
		return fmt.Sprintf(
			"%s %q at %s is allowed by namespace rule",
			setName,
			value.Value,
			value.Path,
		)

	default:
		return fmt.Sprintf(
			"%s %q at %s matched namespace rule action %q",
			setName,
			value.Value,
			value.Path,
			action,
		)
	}
}
