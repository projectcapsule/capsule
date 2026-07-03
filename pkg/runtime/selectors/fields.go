// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

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
}

type CompiledSelectorWithFields struct {
	LabelSelector labels.Selector
	FieldMatchers []CompiledFieldSelector
}

type CompiledFieldSelector struct {
	Raw      string
	Path     string
	Operator FieldSelectorOperator
	Value    string
	Compiled *jsonpath.CompiledJSONPath
}
