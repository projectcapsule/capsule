// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"regexp"
	"slices"
)

// At least one of Exact or Exp must be set.
// Both may be set together.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.exact) || has(self.exp)",message="at least one of exact or exp must be set"
type ExpressionMatch struct {
	ExpressionRegex `json:",inline"`

	// Exact matches one of the provided values exactly.
	//
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Items:MinLength=1
	// +optional
	Exact []string `json:"exact,omitempty"`
}

type ExpressionRegex struct {
	// Exp matches regular expression.
	//
	// +kubebuilder:validation:MinLength=1
	// +optional
	Expression string `json:"exp,omitempty"`
	// Negate regular Expression
	//+kubebuilder:default:=false
	Negate bool `json:"negate,omitempty"`
}

type ExpressionRegexMatcher interface {
	MatchRegex(expression ExpressionRegex, value string) (bool, error)
}

func (m ExpressionMatch) Matches(value string) (bool, error) {
	matched, err := m.matches(value)
	if err != nil {
		return false, err
	}

	return m.applyNegate(matched), nil
}

func (m ExpressionMatch) MatchesWithExpressionMatcher(
	matcher ExpressionRegexMatcher,
	value string,
) (bool, error) {
	if len(m.Exact) == 0 && m.Expression == "" {
		return false, fmt.Errorf("expression match must define at least one of exact or exp")
	}

	matched := containsExact(m.Exact, value)
	if matched {
		return m.applyNegate(true), nil
	}

	if m.Expression == "" {
		return m.applyNegate(false), nil
	}

	if matcher == nil {
		return m.Matches(value)
	}

	matched, err := matcher.MatchRegex(m.ExpressionRegex, value)
	if err != nil {
		return false, err
	}

	// Important: assume MatchRegex already applies ExpressionRegex.Negate.
	// If your RegexCache.MatchRegex already handles Negate, return directly.
	return matched, nil
}

func (m ExpressionMatch) matches(value string) (bool, error) {
	if len(m.Exact) == 0 && m.Expression == "" {
		return false, fmt.Errorf("expression match must define at least one of exact or exp")
	}

	if containsExact(m.Exact, value) {
		return true, nil
	}

	if m.Expression == "" {
		return false, nil
	}

	re, err := regexp.Compile(m.Expression)
	if err != nil {
		return false, fmt.Errorf("compile regexp %q: %w", m.Expression, err)
	}

	return re.MatchString(value), nil
}

func containsExact(values []string, value string) bool {
	return slices.Contains(values, value)
}

func (m ExpressionMatch) applyNegate(matched bool) bool {
	if m.Negate {
		return !matched
	}

	return matched
}
