// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
)

type FieldSelectorOperator string

const (
	FieldSelectorTruthy    FieldSelectorOperator = "truthy"
	FieldSelectorEquals    FieldSelectorOperator = "equals"
	FieldSelectorNotEquals FieldSelectorOperator = "not-equals"
)

// +kubebuilder:object:generate=true
type SelectorWithFields struct {
	// Select Items based on their labels.
	*metav1.LabelSelector `json:",inline"`

	// Additional boolean JSONPath expressions.
	// All must evaluate to true for this selector to match.
	// +optional
	FieldSelectors []string `json:"fieldSelectors,omitempty"`

	// Additional CEL expressions evaluated against the selected object.
	// The object is available as "object".
	// All must evaluate to true for this selector to match.
	// CEL expressions and fieldSelectors may be used together.
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=4096
	// +optional
	CELExpressions []string `json:"celExpressions,omitempty"`
}

type CompiledSelectorWithFields struct {
	LabelSelector labels.Selector
	FieldMatchers []CompiledFieldSelector
	CELMatchers   []*celruntime.CompiledExpression
}

type CompiledFieldSelector struct {
	Raw      string
	Path     string
	Operator FieldSelectorOperator
	Value    string
	Compiled *jsonpath.CompiledJSONPath
}
