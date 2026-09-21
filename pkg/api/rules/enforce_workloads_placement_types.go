// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

// PlacementExpressionMatch uses the shared exact/exp/negate matching semantics.
// The rule admission validator requires exact or exp. Keeping this check out of
// CEL avoids multiplying its estimated cost across the nested placement lists
// and the three representations of namespace rules in RuleStatus.
// +kubebuilder:object:generate=true
type PlacementExpressionMatch struct {
	runtime.ExpressionRegex `json:",inline"`

	// Exact matches one of the provided values exactly.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +optional
	Exact []string `json:"exact,omitempty"`
}

// WorkloadNodeSelectorMatch matches a nodeSelector key/value pair. Omitted fields
// are unrestricted, so {} matches every entry, including an empty label value.
// +kubebuilder:object:generate=true
type WorkloadNodeSelectorMatch struct {
	// +optional
	Key *PlacementExpressionMatch `json:"key,omitempty"`
	// +optional
	Values *PlacementExpressionMatch `json:"values,omitempty"`
}

// PlacementRange defines inclusive bounds. An omitted bound is unrestricted.
// +kubebuilder:object:generate=true
type PlacementRange struct {
	// +optional
	Min *int64 `json:"min,omitempty"`
	// +optional
	Max *int64 `json:"max,omitempty"`
}

// TolerationDurationMatch treats an absent tolerationSeconds as unlimited.
// +kubebuilder:object:generate=true
type TolerationDurationMatch struct {
	PlacementRange `json:",inline"`

	// AllowUnlimited defaults to true. Set false to require a finite duration.
	// +optional
	AllowUnlimited *bool `json:"allowUnlimited,omitempty"`
}

// +kubebuilder:object:generate=true
type WorkloadTolerationMatch struct {
	WorkloadNodeSelectorMatch `json:",inline"`

	// Operators matches Equal (including an omitted operator) or Exists.
	// +optional
	// +kubebuilder:validation:items:Enum=Equal;Exists
	Operators []corev1.TolerationOperator `json:"operators,omitempty"`
	// Effects matches the literal effect. An empty effect on a Pod tolerates all
	// effects and does not match an allowlist of individual effects.
	// +optional
	// +kubebuilder:validation:items:Enum=NoSchedule;PreferNoSchedule;NoExecute;""
	Effects []corev1.TaintEffect `json:"effects,omitempty"`
	// +optional
	TolerationSeconds *TolerationDurationMatch `json:"tolerationSeconds,omitempty"`
}

// PlacementRequirementMatch matches one selector requirement. Every supplied
// value must match Values. Operators that have no values remain valid when
// explicitly permitted. Node selectors additionally support Gt and Lt.
// +kubebuilder:object:generate=true
type PlacementRequirementMatch struct {
	WorkloadNodeSelectorMatch `json:",inline"`

	// +optional
	// +kubebuilder:validation:items:Enum=In;NotIn;Exists;DoesNotExist;Gt;Lt
	Operators []corev1.NodeSelectorOperator `json:"operators,omitempty"`
}

// PlacementLabelSelectorMatch checks every effective selector requirement.
// matchLabels is normalized to In, and dynamic matchLabelKeys/mismatchLabelKeys
// are checked as In/NotIn using the incoming Pod's label values. Missing dynamic
// labels are ignored, matching Kubernetes semantics.
// +kubebuilder:object:generate=true
type PlacementLabelSelectorMatch struct {
	// Required requires at least one effective selector requirement.
	// +optional
	Required bool `json:"required,omitempty"`
	// Requirements is an allowlist within this matcher. If empty, requirements
	// are unrestricted. Each actual requirement must match one complete entry.
	// +optional
	Requirements []PlacementRequirementMatch `json:"requirements,omitempty"`
}

// +kubebuilder:object:generate=true
type WorkloadTopologySpreadMatch struct {
	// +optional
	TopologyKey *PlacementExpressionMatch `json:"topologyKey,omitempty"`
	// +optional
	// +kubebuilder:validation:items:Enum=DoNotSchedule;ScheduleAnyway
	WhenUnsatisfiable []corev1.UnsatisfiableConstraintAction `json:"whenUnsatisfiable,omitempty"`
	// +optional
	MaxSkew *PlacementRange `json:"maxSkew,omitempty"`
	// MinDomains uses 1 when the Pod omits minDomains.
	// +optional
	MinDomains *PlacementRange `json:"minDomains,omitempty"`
	// NodeAffinityPolicy uses Honor when the Pod omits the field.
	// +optional
	// +kubebuilder:validation:items:Enum=Honor;Ignore
	NodeAffinityPolicy []corev1.NodeInclusionPolicy `json:"nodeAffinityPolicy,omitempty"`
	// NodeTaintsPolicy uses Ignore when the Pod omits the field.
	// +optional
	// +kubebuilder:validation:items:Enum=Honor;Ignore
	NodeTaintsPolicy []corev1.NodeInclusionPolicy `json:"nodeTaintsPolicy,omitempty"`
	// +optional
	LabelSelector *PlacementLabelSelectorMatch `json:"labelSelector,omitempty"`
}

// +kubebuilder:validation:Enum=nodeAffinity;podAffinity;podAntiAffinity
type PlacementAffinityType string

const (
	PlacementNodeAffinity    PlacementAffinityType = "nodeAffinity"
	PlacementPodAffinity     PlacementAffinityType = "podAffinity"
	PlacementPodAntiAffinity PlacementAffinityType = "podAntiAffinity"
)

// +kubebuilder:validation:Enum=required;preferred
type PlacementAffinityMode string

const (
	PlacementAffinityRequired  PlacementAffinityMode = "required"
	PlacementAffinityPreferred PlacementAffinityMode = "preferred"
)

// +kubebuilder:validation:Enum=SameNamespace;Any
type PlacementNamespaceScope string

const (
	PlacementSameNamespace PlacementNamespaceScope = "SameNamespace"
	PlacementAnyNamespace  PlacementNamespaceScope = "Any"
)

// WorkloadAffinityMatch matches an entire affinity term. Fields are ANDed;
// entries in enforce.workloads.affinity are alternatives. {} matches any term.
// +kubebuilder:object:generate=true
type WorkloadAffinityMatch struct {
	// Types defaults to all types. Type-specific constraints only match the
	// types on which they are meaningful.
	// +optional
	Types []PlacementAffinityType `json:"types,omitempty"`
	// Modes defaults to both scheduling modes; it does not require their presence.
	// +optional
	Modes []PlacementAffinityMode `json:"modes,omitempty"`
	// Weight requires modes: [preferred].
	// +optional
	Weight *PlacementRange `json:"weight,omitempty"`
	// Requirements constrains node matchExpressions.
	// +optional
	Requirements []PlacementRequirementMatch `json:"requirements,omitempty"`
	// FieldRequirements constrains node matchFields. When Requirements is set,
	// matchFields must be explicitly permitted here to avoid an unchecked path.
	// +optional
	FieldRequirements []PlacementRequirementMatch `json:"fieldRequirements,omitempty"`
	// TopologyKey applies to Pod affinity and anti-affinity.
	// +optional
	TopologyKey *PlacementExpressionMatch `json:"topologyKey,omitempty"`
	// SameNamespace requires no namespaceSelector and only the Pod's namespace
	// in namespaces (or an omitted namespaces list). Any imposes no restriction.
	// +optional
	NamespaceScope PlacementNamespaceScope `json:"namespaceScope,omitempty"`
	// Namespaces constrains each explicitly supplied namespace.
	// +optional
	Namespaces *PlacementExpressionMatch `json:"namespaces,omitempty"`
	// +optional
	NamespaceSelector *PlacementLabelSelectorMatch `json:"namespaceSelector,omitempty"`
	// +optional
	LabelSelector *PlacementLabelSelectorMatch `json:"labelSelector,omitempty"`
}
