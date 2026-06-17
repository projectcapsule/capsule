// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"regexp"
)

// +kubebuilder:object:generate=true
type RegExpression struct {
	// Expression used to evaluate regex
	Expression string `json:"exp,omitempty"`
	// Negate regular Expression
	//+kubebuilder:default:=false
	Negate bool `json:"negate,omitempty"`
}

// Exactly one of Name or Exp should be set.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.name) != has(self.exp)",message="exactly one of name or exp must be set"
type ExpressionMatch struct {
	// Name matches exactly
	//
	// +kubebuilder:validation:MinLength=1
	// +optional
	Name string `json:"name,omitempty"`

	// Exp matches regular expression.
	//
	// +kubebuilder:validation:MinLength=1
	// +optional
	Expression string `json:"exp,omitempty"`
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
