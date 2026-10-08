// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceNetworkBody struct {
	// Policies constrains native NetworkPolicies.
	// +optional
	Policies NamespaceRuleEnforceNetworkPoliciesBody `json:"policies,omitempty"`
}

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceNetworkPoliciesBody struct {
	// Ingress constrains source address grants. Selector-based peers are
	// unaffected. No constraints are applied when omitted.
	// +optional
	Ingress *NetworkPolicyCIDRRule `json:"ingress,omitempty"`

	// Egress constrains destination address grants. Selector-based peers are
	// unaffected. No constraints are applied when omitted.
	// +optional
	Egress *NetworkPolicyCIDRRule `json:"egress,omitempty"`
}

// +kubebuilder:object:generate=true
type NetworkPolicyCIDRRule struct {
	// CIDRs matches addresses granted by ipBlock.cidr after subtracting except.
	// Omitted peers on an ingress or egress rule grant both IPv4 and IPv6 address
	// space. An empty NetworkPolicy ingress or egress list has no grants in that
	// direction to validate. Each direction is evaluated independently.
	// An empty CIDRs list imposes no constraint.
	// Ordered allow/deny decisions apply to every granted address: deny rejects
	// overlapping grants, allow requires complete coverage, and audit reports
	// overlaps. A later allow cannot authorize addresses outside its CIDRs.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=49
	// +listType=set
	CIDRs []string `json:"cidrs,omitempty"`
}
