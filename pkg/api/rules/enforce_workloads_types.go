// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// +kubebuilder:validation:Enum=pod/initcontainers;pod/ephemeralcontainers;pod/containers;pod/volumes
type WorkloadValidationTarget string

const (
	DeprecatedValidateImages WorkloadValidationTarget = "pod/images"

	ValidateInitContainers      WorkloadValidationTarget = "pod/initcontainers"
	ValidateEphemeralContainers WorkloadValidationTarget = "pod/ephemeralcontainers"
	ValidateContainers          WorkloadValidationTarget = "pod/containers"
	ValidateVolumes             WorkloadValidationTarget = "pod/volumes"
)

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceWorkloadsBody struct {
	// Define the enforcement targets this rule applies to.
	// If empty, each webhook applies its own backwards-compatible default.
	// +optional
	Targets []WorkloadValidationTarget `json:"targets,omitempty"`

	// Define Pod QoS classes matched by this enforcement rule.
	// Supported values are Guaranteed, Burstable and BestEffort.
	// +optional
	QoSClasses []corev1.PodQOSClass `json:"qosClasses,omitempty"`

	// Define registries which are allowed to be used within this tenant
	// The rules are aggregated, since you can use Regular Expressions the match registry endpoints
	// +optional
	Registries []OCIRegistry `json:"registries,omitempty"`

	// Schedulers defines schedulerName matchers for Pod admission.
	//
	// The rule is evaluated against pod.spec.schedulerName.
	// Empty schedulerName is ignored and is not normalized to default-scheduler.
	//
	// +optional
	Schedulers []runtime.ExpressionMatch `json:"schedulers,omitempty"`
}
