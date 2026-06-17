// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"regexp"
)

// Exactly one of Name or Exp should be set.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.name) != has(self.exp)",message="exactly one of name or exp must be set"
type ExpressionMatch struct {
	ExpressionRegex `json:",inline"`

	// Name matches exactly
	//
	// +kubebuilder:validation:MinLength=1
	// +optional
	Name string `json:"name,omitempty"`
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
	MatchRegex(ExpressionRegex, string) (bool, error)
}

func (m ExpressionMatch) Matches(value string) (bool, error) {
	switch {
	case m.Name != "":
		return m.Name == value, nil

	case m.Expression != "":
		re, err := regexp.Compile(m.Expression)
		if err != nil {
			return false, fmt.Errorf("compile regexp %q: %w", m.Expression, err)
		}

		return re.MatchString(value), nil

	default:
		return false, fmt.Errorf("expression match must define either name or exp")
	}
}

func (m ExpressionMatch) MatchesWithExpressionMatcher(matcher ExpressionRegexMatcher, value string) (bool, error) {
	if m.Name != "" {
		matched := m.Name == value
		if m.Negate {
			return !matched, nil
		}

		return matched, nil
	}

	if m.Expression == "" {
		return false, fmt.Errorf("expression match must define either name or exp")
	}

	if matcher == nil {
		return m.Matches(value)
	}

	return matcher.MatchRegex(m.ExpressionRegex, value)
}
