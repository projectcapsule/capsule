// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// NamespaceRuleEnforceDevicesBody controls DRA class references, including every
// firstAvailable alternative. It does not grant administrative device access.
// +kubebuilder:object:generate=true
type NamespaceRuleEnforceDevicesBody struct {
	// Classes matches DeviceClasses by name or existing class labels. Entries are
	// ORed; each requested class is evaluated independently in rule order.
	// An applicable allow list rejects unmatched classes. Audit never grants access.
	// Legacy Tenant.spec.deviceClasses restrictions remain an additional boundary.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Classes []DeviceClassMatch `json:"classes,omitempty"`
}

// DeviceClassMatch ORs exact names, a name expression, and a label selector.
// At least one criterion is required by rule admission validation. As with
// placement expressions, this is checked outside CEL to bound schema cost.
// +kubebuilder:object:generate=true
type DeviceClassMatch struct {
	// Exp and negate use the shared name-expression semantics. Negate applies
	// to the name match only, independently of the optional label selector.
	runtime.ExpressionRegex `json:",inline"`

	// Exact matches one of these class names.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	Exact []string `json:"exact,omitempty"`

	// Selector matches labels on the DeviceClass, not the claim or namespace.
	// Omitted means no label criterion; explicit {} matches every existing class.
	// References must resolve when selectors participate in allow/deny enforcement.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// NameMatch projects the shared name matcher without copying the exact-name list.
func (m DeviceClassMatch) NameMatch() runtime.ExpressionMatch {
	return runtime.ExpressionMatch{ExpressionRegex: m.ExpressionRegex, Exact: m.Exact}
}
