// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

// +kubebuilder:object:generate=true
type RegExpression struct {
	// Expression used to evaluate regex
	Expression string `json:"exp,omitempty"`
	// Negate regular Expression
	//+kubebuilder:default:=false
	Negate bool `json:"negate,omitempty"`
}
