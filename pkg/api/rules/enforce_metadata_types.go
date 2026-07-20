// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// MetadataRule defines metadata constraints for namespaced resources.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.labels) || has(self.annotations)",message="at least one of labels or annotations must be set"
type MetadataRule struct {
	runtime.VersionKinds `json:",inline"`

	// Labels defines metadata policies by label key.
	//
	// +optional
	Labels map[string]MetadataValueRule `json:"labels,omitempty"`

	// Annotations defines metadata policies by annotation key.
	//
	// +optional
	Annotations map[string]MetadataValueRule `json:"annotations,omitempty"`
}

// MatchesGroupVersionKind matches metadata targets. Namespace is deliberately
// opt-in: wildcard kind selectors never include it, so cluster-scoped
// namespace admission cannot be enabled accidentally.
func (r MetadataRule) MatchesGroupVersionKind(gvk schema.GroupVersionKind) bool {
	if gvk.Group == "" && gvk.Version == "v1" && gvk.Kind == "Namespace" {
		explicit := false
		for _, kind := range r.Kinds {
			if strings.TrimSpace(kind) == "Namespace" {
				explicit = true
				break
			}
		}
		if !explicit {
			return false
		}
	}

	return r.VersionKinds.MatchesGroupVersionKind(gvk)
}

// +kubebuilder:object:generate=true
type MetadataValueRule struct {
	// Required enforces that the metadata key must be present.
	//
	// This is mainly meaningful with action=allow. Deny and audit rules remain
	// value matchers and do not require missing metadata to exist.
	//
	// +optional
	// +kubebuilder:default:=false
	Required bool `json:"required,omitempty"`

	// Values defines allowed, denied, or audited values for the metadata key.
	//
	// If Required=true and Values is empty, only presence is enforced.
	//
	// +optional
	Values []runtime.ExpressionMatch `json:"values,omitempty"`

	// Default is applied by admission mutation when the concrete metadata key is absent.
	// It is not reconciled after admission.
	// +optional
	Default *string `json:"default,omitempty"`

	// Managed is enforced by admission mutation and continuously reconciled by
	// the RuleStatus controller using server-side apply.
	// +optional
	Managed *string `json:"managed,omitempty"`
}

// MetadataKeyExpression converts a metadata key selector into the regular
// expression used by admission validation and runtime matching. Asterisks are
// convenient wildcards, while the rest of the selector retains regexp syntax.
func MetadataKeyExpression(selector string) runtime.ExpressionRegex {
	selector = strings.TrimSpace(selector)
	var expression strings.Builder
	for i, char := range selector {
		if char == '*' && (i == 0 || selector[i-1] != '.') {
			expression.WriteString(".*")
			continue
		}
		expression.WriteRune(char)
	}

	return runtime.ExpressionRegex{
		Expression: "^(?:" + expression.String() + ")$",
	}
}
