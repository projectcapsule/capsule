// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type ImagePullPolicySpec string

func (i ImagePullPolicySpec) String() string {
	return string(i)
}

// +kubebuilder:object:generate=true
type OCIRegistry struct {
	runtime.ExpressionMatch `json:",inline"`

	// Allowed PullPolicy for the given registry. Supplying no value allows all policies.
	// +optional
	// +kubebuilder:validation:Items:Enum=Always;Never;IfNotPresent
	Policy []corev1.PullPolicy `json:"policy,omitempty"`
}
