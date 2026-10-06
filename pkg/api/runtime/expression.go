// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxExpressionMatchExactValues = 64
	MaxExpressionRegexLength      = 4096
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
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:Items:MinLength=1
	// +optional
	Exact []string `json:"exact,omitempty"`
}

type ExpressionRegex struct {
	// Exp matches regular expression.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	Expression string `json:"exp,omitempty"`
	// Negate regular Expression
	//+kubebuilder:default:=false
	Negate bool `json:"negate,omitempty"`
}

type ExpressionRegexMatcher interface {
	MatchRegex(expression ExpressionRegex, value string) (bool, error)
}

// ValidateLimits checks policy size limits without compiling the expression.
// Length is measured in Unicode characters to match Kubernetes schema validation.
func (m ExpressionMatch) ValidateLimits(fieldPath string) error {
	if len(m.Exact) > MaxExpressionMatchExactValues {
		return fmt.Errorf("%s.exact: at most %d values are supported", fieldPath, MaxExpressionMatchExactValues)
	}

	if len(m.Expression) > MaxExpressionRegexLength && utf8.RuneCountInString(m.Expression) > MaxExpressionRegexLength {
		return fmt.Errorf("%s.exp: at most %d characters are supported", fieldPath, MaxExpressionRegexLength)
	}

	return nil
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

func (m ExpressionMatch) Describe() string {
	return DescribeExpressionMatch(m)
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

func DescribeExpressionMatch(match ExpressionMatch) string {
	parts := make([]string, 0, 3)

	prefix := ""
	if match.Negate {
		prefix = "not "
	}

	if len(match.Exact) > 0 {
		parts = append(parts, fmt.Sprintf("%sexact: %s", prefix, strings.Join(match.Exact, ", ")))
	}

	if match.Expression != "" {
		parts = append(parts, fmt.Sprintf("%sexp: %s", prefix, match.Expression))
	}

	if len(parts) == 0 && match.Negate {
		return "not <empty>"
	}

	return strings.Join(parts, "; ")
}
