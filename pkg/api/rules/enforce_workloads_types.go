// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// +kubebuilder:validation:Enum=pod;pod/initcontainers;pod/ephemeralcontainers;pod/containers;pod/volumes
type WorkloadValidationTarget string

const (
	DeprecatedValidateImages WorkloadValidationTarget = "pod/images"

	ValidatePod                 WorkloadValidationTarget = "pod"
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

	// Resources defines mutation and enforcement policies for Pod and container
	// resource requests and limits. The workload targets select where the
	// policies apply. With no targets, resource policies apply to regular and
	// init containers. Pod-level resources require the explicit "pod" target.
	// Mutation is applied when a Pod is created. Remove and MatchRequest manage
	// explicit values, Default fills an absent value, and Ratio fills an absent
	// limit from its request. An explicit Ratio violation is then handled by the
	// enclosing allow, deny, or audit action.
	//
	// +optional
	Resources *WorkloadResourceRules `json:"resources,omitempty"`

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

type WorkloadResourceRequestPolicyType string

const (
	WorkloadResourceRequestPolicyPreserve WorkloadResourceRequestPolicyType = "Preserve"
	WorkloadResourceRequestPolicyDefault  WorkloadResourceRequestPolicyType = "Default"
	WorkloadResourceRequestPolicyRemove   WorkloadResourceRequestPolicyType = "Remove"
)

type WorkloadResourceLimitPolicyType string

const (
	WorkloadResourceLimitPolicyPreserve     WorkloadResourceLimitPolicyType = "Preserve"
	WorkloadResourceLimitPolicyDefault      WorkloadResourceLimitPolicyType = "Default"
	WorkloadResourceLimitPolicyRemove       WorkloadResourceLimitPolicyType = "Remove"
	WorkloadResourceLimitPolicyMatchRequest WorkloadResourceLimitPolicyType = "MatchRequest"
	WorkloadResourceLimitPolicyRatio        WorkloadResourceLimitPolicyType = "Ratio"
)

// WorkloadResourceRules defines policies keyed by Kubernetes resource name.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="has(self.requests) || has(self.limits)",message="at least one of requests or limits must be set"
type WorkloadResourceRules struct {
	// Requests defines policies for resource requests.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	Requests map[corev1.ResourceName]WorkloadResourceRequestPolicy `json:"requests,omitempty"`

	// Limits defines policies for resource limits.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	Limits map[corev1.ResourceName]WorkloadResourceLimitPolicy `json:"limits,omitempty"`
}

// WorkloadResourceRequestPolicy defines how a resource request is mutated.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.policy == 'Default' ? has(self.value) : !has(self.value)",message="value must be set only for the Default policy"
type WorkloadResourceRequestPolicy struct {
	// Policy selects how the request is handled: Preserve leaves it unchanged,
	// Default fills an absent request, and Remove deletes it.
	// +kubebuilder:validation:Enum=Preserve;Default;Remove
	Policy WorkloadResourceRequestPolicyType `json:"policy"`

	// Value is the quantity applied by the Default policy.
	// +optional
	Value *resource.Quantity `json:"value,omitempty"`
}

// WorkloadResourceLimitPolicy defines how a resource limit is mutated and enforced.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.policy == 'Default' || self.policy == 'Ratio' ? has(self.value) : !has(self.value)",message="value must be set only for the Default and Ratio policies"
type WorkloadResourceLimitPolicy struct {
	// Policy selects how the limit is handled: Preserve leaves it unchanged,
	// Default fills an absent limit, Remove deletes it, MatchRequest manages it
	// to equal the request, and Ratio defaults an absent limit and enforces the
	// maximum multiplier against explicitly supplied limits.
	// +kubebuilder:validation:Enum=Preserve;Default;Remove;MatchRequest;Ratio
	Policy WorkloadResourceLimitPolicyType `json:"policy"`

	// Value is the quantity applied by Default or the maximum limit-to-request
	// multiplier applied by Ratio.
	// +optional
	Value *resource.Quantity `json:"value,omitempty"`
}
