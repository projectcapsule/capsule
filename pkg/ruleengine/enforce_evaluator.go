// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	api "github.com/projectcapsule/capsule/pkg/api/rules"
)

type Value struct {
	Value string
	Path  string
}

type Match struct {
	Matched      bool
	MatchedValue any

	// Detail is optional human-readable matcher context.
	// Example: "10.0.171.239 is contained in 10.0.0.0/16".
	Detail string
}

type Decision struct {
	SetName     string
	EventReason string
	Action      api.ActionType
	Value       Value

	MatchedValue any

	// MatchedRule is the human-readable rule description returned by Set.RuleDescription.
	MatchedRule string

	// MatchDetail is the human-readable detail returned by Match.Detail.
	MatchDetail string

	Message string
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

type Set[R any, O any] struct {
	Name        string
	EventReason string

	Values  func(O) []Value
	Rules   func(*api.NamespaceRuleEnforceBody) []R
	Matches func(R, Value) (Match, error)

	// Message can fully override the default message.
	// Prefer leaving this nil unless a rule requires very specific wording.
	Message func(api.ActionType, Value, any) string

	// RuleDescription returns a human-readable representation of one rule.
	// It is used only for admission/audit messages.
	RuleDescription func(R) string

	// AllowedDescription optionally overrides the "Allowed values" label.
	// Example: "Allowed CIDRs", "Allowed ranges", "Allowed hostnames".
	AllowedDescription string
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
		allowRules := make([]R, 0)

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

				allowRules = append(allowRules, items...)

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

				matchedRule := describeRule(set, item)

				decision := &Decision{
					SetName:      set.Name,
					EventReason:  set.EventReason,
					Action:       action,
					Value:        value,
					MatchedValue: match.MatchedValue,
					MatchedRule:  matchedRule,
					MatchDetail:  strings.TrimSpace(match.Detail),
					Message: decisionMessage(
						set,
						action,
						value,
						match.MatchedValue,
						matchedRule,
						match.Detail,
					),
				}

				switch action {
				case api.ActionTypeAudit:
					// Audit is purely observational. It must not influence
					// allow/deny evaluation.
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
				Message:     allowMissMessage(set, value, allowRules),
			}

			return evaluation, nil
		}
	}

	return evaluation, nil
}

const maxRuleDescriptions = 10

func describeRule[R any, O any](set Set[R, O], rule R) string {
	if set.RuleDescription == nil {
		return ""
	}

	return strings.TrimSpace(set.RuleDescription(rule))
}

func describeRules[R any, O any](set Set[R, O], rules []R) string {
	if len(rules) == 0 || set.RuleDescription == nil {
		return ""
	}

	limit := min(len(rules), maxRuleDescriptions)

	parts := make([]string, 0, limit)

	for i := range limit {
		description := describeRule(set, rules[i])
		if description == "" {
			continue
		}

		parts = append(parts, description)
	}

	if len(parts) == 0 {
		return ""
	}

	if len(rules) > maxRuleDescriptions {
		parts = append(parts, fmt.Sprintf("and %d more", len(rules)-maxRuleDescriptions))
	}

	return strings.Join(parts, ", ")
}

func allowedLabel[R any, O any](set Set[R, O]) string {
	if set.AllowedDescription != "" {
		return set.AllowedDescription
	}

	return "Allowed values"
}

func allowMissMessage[R any, T any](
	set Set[R, T],
	value Value,
	allowRules []R,
) string {
	message := fmt.Sprintf(
		"%s %q at %s is not allowed by namespace rule",
		set.Name,
		value.Value,
		value.Path,
	)

	descriptions := describeRules(set, allowRules)
	if descriptions == "" {
		return message
	}

	return fmt.Sprintf(
		"%s: value did not match any allowed rule. %s: %s",
		message,
		allowedLabel(set),
		descriptions,
	)
}

func decisionMessage[R any, T any](
	set Set[R, T],
	action api.ActionType,
	value Value,
	matchedValue any,
	matchedRule string,
	matchDetail string,
) string {
	if set.Message != nil {
		return set.Message(action, value, matchedValue)
	}

	matchDetail = strings.TrimSpace(matchDetail)

	switch action {
	case api.ActionTypeAudit:
		message := fmt.Sprintf(
			"%s %q at %s matched audit namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

		return appendMatchContext(message, matchedRule, matchDetail, "matched audit rule")

	case api.ActionTypeDeny:
		message := fmt.Sprintf(
			"%s %q at %s is denied by namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

		return appendMatchContext(message, matchedRule, matchDetail, "matched denied rule")

	case api.ActionTypeAllow:
		message := fmt.Sprintf(
			"%s %q at %s is allowed by namespace rule",
			set.Name,
			value.Value,
			value.Path,
		)

		return appendMatchContext(message, matchedRule, matchDetail, "matched allowed rule")

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

func appendMatchContext(
	message string,
	matchedRule string,
	matchDetail string,
	rulePrefix string,
) string {
	if matchDetail != "" {
		return fmt.Sprintf("%s: %s", message, matchDetail)
	}

	if matchedRule != "" {
		return fmt.Sprintf("%s: %s %s", message, rulePrefix, matchedRule)
	}

	return message
}
