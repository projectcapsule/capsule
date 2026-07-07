// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import "github.com/projectcapsule/capsule/pkg/api/runtime"

// +kubebuilder:object:generate=true
type NamespaceRuleEnforceServicesBody struct {
	// Types defines the Service types matched by this rule.
	//
	// Supported values:
	// - ClusterIP
	// - NodePort
	// - LoadBalancer
	// - ExternalName
	//
	// +optional
	// +kubebuilder:validation:items:Enum=ClusterIP;NodePort;LoadBalancer;ExternalName
	Types []ServiceType `json:"types,omitempty"`

	// LoadBalancers defines additional constraints for Services of type LoadBalancer.
	// +optional
	LoadBalancers *ServiceLoadBalancerRule `json:"loadBalancers,omitempty"`

	// ExternalNames defines additional constraints for Services of type ExternalName.
	// +optional
	ExternalNames *ServiceExternalNameRule `json:"externalNames,omitempty"`

	// NodePorts defines additional constraints for nodePort values.
	// +optional
	NodePorts *ServiceNodePortRule `json:"nodePorts,omitempty"`
}

// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer;ExternalName
type ServiceType string

const (
	ServiceTypeClusterIP    ServiceType = "ClusterIP"
	ServiceTypeNodePort     ServiceType = "NodePort"
	ServiceTypeLoadBalancer ServiceType = "LoadBalancer"
	ServiceTypeExternalName ServiceType = "ExternalName"
)

// +kubebuilder:object:generate=true
type ServiceLoadBalancerRule struct {
	// CIDRs restricts spec.loadBalancerIP and spec.loadBalancerSourceRanges.
	// Empty means no additional CIDR restriction once LoadBalancer is allowed by types.
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`
}

// +kubebuilder:object:generate=true
type ServiceExternalNameRule struct {
	// Hostnames restricts spec.externalName.
	// Empty means no additional hostname restriction once ExternalName is allowed by types.
	// +optional
	Hostnames []runtime.ExpressionMatch `json:"hostnames,omitempty"`
}

// +kubebuilder:object:generate=true
type ServiceNodePortRule struct {
	// Ports restricts explicitly requested nodePort values.
	// Empty means no additional port restriction once NodePort is allowed by types.
	// +optional
	Ports []ServiceNodePortRange `json:"ports,omitempty"`
}

// +kubebuilder:object:generate=true
type ServiceNodePortRange struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	From int32 `json:"from"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	To int32 `json:"to"`
}
