package rules

import (
	"fmt"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

type Value struct {
	Value string
	Path  string
}

type Match struct {
	Matched      bool
	MatchedValue any
}

type Decision struct {
	SetName      string
	EventReason  string
	Action       api.ActionType
	Value        Value
	MatchedValue any
	Message      string
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
	// Final is the last matching allow/deny decision.
	Final *Decision

	// Blocking is set when the final result blocks admission.
	Blocking *Decision

	// Audits contains all matching audit decisions.
	Audits []*Decision
}

func (e *Evaluation) BlockingError() error {
	if e == nil || e.Blocking == nil {
		return nil
	}

	return &DecisionError{
		Decision: e.Blocking,
	}
}

func (e *Evaluation) Append(other *Evaluation) {
	if e == nil || other == nil {
		return
	}

	e.Audits = append(e.Audits, other.Audits...)

	if other.Final != nil {
		e.Final = other.Final
	}

	if other.Blocking != nil {
		e.Blocking = other.Blocking
	}
}

type Set[R any, T any] struct {
	Name string

	EventReason string

	Values func(T) []Value

	Rules func(*api.NamespaceRuleEnforceBody) []R

	Matches func(R, Value) (Match, error)

	Message func(action api.ActionType, value Value, matchedValue any) string
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

			action := enforce.Action.OrDefault()

			switch action {
			case api.ActionTypeAllow:
				hasAllowRule = true

			case api.ActionTypeDeny, api.ActionTypeAudit:
				// Supported actions.

			default:
				return evaluation, fmt.Errorf(
					"%s: unsupported rule action %q",
					set.Name,
					action,
				)
			}

			for _, item := range items {
				match, err := set.Matches(item, value)
				if err != nil {
					return evaluation, fmt.Errorf("%s: invalid rule: %w", set.Name, err)
				}

				if !match.Matched {
					continue
				}

				decision := &Decision{
					SetName:      set.Name,
					EventReason:  set.EventReason,
					Action:       action,
					Value:        value,
					MatchedValue: match.MatchedValue,
					Message:      decisionMessage(set, action, value, match.MatchedValue),
				}

				switch action {
				case api.ActionTypeAudit:
					evaluation.Audits = append(evaluation.Audits, decision)

				case api.ActionTypeAllow, api.ActionTypeDeny:
					// Last matching allow/deny wins.
					lastDecision = decision
				}
			}
		}

		if lastDecision != nil {
			evaluation.Final = lastDecision

			if lastDecision.Action == api.ActionTypeDeny {
				evaluation.Blocking = lastDecision
				return evaluation, nil
			}

			continue
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

func decisionMessage[R any, T any](
	set Set[R, T],
	action api.ActionType,
	value Value,
	matchedValue any,
) string {
	if set.Message != nil {
		return set.Message(action, value, matchedValue)
	}

	switch action {
	case api.ActionTypeAudit:
		return fmt.Sprintf(
			"%s %q at %s matched audit namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

	case api.ActionTypeDeny:
		return fmt.Sprintf(
			"%s %q at %s is denied by namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

	case api.ActionTypeAllow:
		return fmt.Sprintf(
			"%s %q at %s is allowed by namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

	default:
		return fmt.Sprintf(
			"%s %q at %s matched namespace rule action %q",
			set.Name,
			value.Value,
			value.Path,
			action,
		)
	}
}
