// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceWorkloadsBody struct {
	// SeccompProfiles matches effective Linux container profiles, resolving
	// container overrides before Pod defaults. Privileged containers are Unconfined.
	// Missing profiles do not match any type, so an allow-list rejects them.
	// Omitted targets check regular, init and ephemeral containers; pod explicitly
	// selects only the Pod default. Controller targets check their Pod templates.
	// +optional
	SeccompProfiles []WorkloadSecurityProfileMatch `json:"seccompProfiles,omitempty"`

	// AppArmorProfiles matches effective Linux container profiles, including
	// legacy AppArmor annotations before Pod defaults. It uses the same target
	// and missing-profile semantics as SeccompProfiles. Privileged containers are
	// Unconfined. Matching a Localhost name does not verify its installation.
	// +optional
	AppArmorProfiles []WorkloadSecurityProfileMatch `json:"appArmorProfiles,omitempty"`

	// Targets selects native workloads and, optionally, parts of their Pod specs.
	// With no workload policies, the action matches the selected kinds themselves.
	// With policies, targets scopes those policies; it does not also match the kind.
	// Omitted targets preserve Pod-only defaults. Controller targets are opt-in.
	// Whole-controller targets include every compatible location in the Pod template.
	// Existing pod and pod/* part targets retain their established policy scope.
	// +optional
	Targets []WorkloadValidationTarget `json:"targets,omitempty"`

	// NodeSelector matches each node selector entry. Empty matchers match any entry.
	// Placement rules apply to the pod target; allowing an entry does not require it.
	// +optional
	NodeSelector []WorkloadNodeSelectorMatch `json:"nodeSelector,omitempty"`

	// Tolerations matches each toleration, including injected and wildcard tolerations.
	// +optional
	Tolerations []WorkloadTolerationMatch `json:"tolerations,omitempty"`

	// TopologySpreadConstraints matches each complete spread constraint.
	// +optional
	TopologySpreadConstraints []WorkloadTopologySpreadMatch `json:"topologySpreadConstraints,omitempty"`

	// Affinity matches each complete required or preferred affinity term.
	// +optional
	Affinity []WorkloadAffinityMatch `json:"affinity,omitempty"`

	// Resources defines mutation and enforcement policies for Pod and container
	// resource requests and limits. The workload targets select where the
	// policies apply. With no targets, resource policies apply to all compatible
	// locations: Pod-level resources, regular containers, and init containers.
	// Resource names unsupported at Pod level still apply to compatible container
	// locations.
	// Mutation is applied when a Pod is created, or when an explicitly targeted
	// controller is created or updated (to its Pod template). Remove and MatchRequest manage
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

	// Schedulers defines schedulerName matchers for selected Pods and Pod templates.
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
