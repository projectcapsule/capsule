// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// IngressMutation contains typed ingress and Gateway API mutations.
// +kubebuilder:object:generate=true
type IngressMutation struct {
	// ListenerSets configures gateway.networking.k8s.io/v1 ListenerSets.
	// +optional
	ListenerSets ListenerSetMutation `json:"listenerSets,omitzero"`
}

// ListenerSetMutation applies on main-resource CREATE and UPDATE. Rule changes
// do not reconcile existing ListenerSets.
// +kubebuilder:object:generate=true
type ListenerSetMutation struct {
	// ParentRef supplies the complete spec.parentRef. Merge fills only an absent
	// or null reference; any supplied reference is retained, including an omitted
	// namespace. Replace replaces the entire reference. Nil leaves it unchanged.
	// The first applicable merge wins; later replacements can override it.
	// Attachment remains subject to the parent's allowedListeners configuration.
	// +optional
	ParentRef *GatewayParentReference `json:"parentRef,omitempty"`
}

// GatewayParentReference mirrors Gateway API's ParentGatewayReference. String
// fields permit namespace-rule templates; rendered values are validated through
// the existing RuleStatus validation path. Capsule does not read or create the
// referenced resource. An omitted namespace refers to the ListenerSet namespace.
// +kubebuilder:object:generate=true
type GatewayParentReference struct {
	// Group identifies the parent's API group. An explicit empty string selects
	// the core API group, following Gateway API reference semantics.
	// +optional
	// +kubebuilder:default="gateway.networking.k8s.io"
	// +kubebuilder:validation:MaxLength=253
	Group *string `json:"group,omitempty"`

	// Kind identifies the parent's kind.
	// +optional
	// +kubebuilder:default=Gateway
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Kind *string `json:"kind,omitempty"`

	// Name identifies the parent resource.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Namespace identifies the parent's namespace. Omitted means the
	// ListenerSet's namespace; an explicit empty string is invalid.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace *string `json:"namespace,omitempty"`
}
