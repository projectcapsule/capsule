// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// HasNameMatch reports whether exact or exp is set.
func (m StorageClassMatch) HasNameMatch() bool {
	return len(m.Exact) > 0 || m.Expression != ""
}

// HasSelector reports whether matching reads the labels of the class object.
func (m StorageClassMatch) HasSelector() bool {
	return len(m.MatchLabels) > 0 || len(m.MatchExpressions) > 0
}

// NameMatch returns exact and exp as an expression match for the shared regex cache.
func (m StorageClassMatch) NameMatch() runtime.ExpressionMatch {
	return runtime.ExpressionMatch{Expression: m.Expression, Exact: m.Exact}
}

func (m StorageClassMatch) Describe() string {
	parts := make([]string, 0, 2)

	if m.HasNameMatch() {
		parts = append(parts, runtime.DescribeExpressionMatch(m.NameMatch()))
	}

	if m.HasSelector() {
		parts = append(parts, "selector: "+metav1.FormatLabelSelector(&m.LabelSelector))
	}

	return strings.Join(parts, "; ")
}
