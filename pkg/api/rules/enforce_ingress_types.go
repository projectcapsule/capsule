// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import "github.com/projectcapsule/capsule/pkg/api/runtime"

// +kubebuilder:validation:Enum=Ingress;Route;ListenerSet;HTTPRoute;Gateway;TLSRoute;GRPCRoute
type IngressType string

const (
	IngressTypeIngress     IngressType = "Ingress"
	IngressTypeRoute       IngressType = "Route"
	IngressTypeListenerSet IngressType = "ListenerSet"
	IngressTypeHTTPRoute   IngressType = "HTTPRoute"
	IngressTypeGateway     IngressType = "Gateway"
	IngressTypeTLSRoute    IngressType = "TLSRoute"
	IngressTypeGRPCRoute   IngressType = "GRPCRoute"
)

// NamespaceRuleEnforceIngressBody defines hostname enforcement for Kubernetes
// Ingress and Gateway API resources.
//
// +kubebuilder:object:generate=true
type NamespaceRuleEnforceIngressBody struct {
	// Types defines the resource kinds to which hostname enforcement applies.
	//
	// +kubebuilder:validation:MinItems=1
	Types []IngressType `json:"types,omitempty"`

	// Hostnames defines allowed, denied, or audited hostname expressions.
	// A resource targeted by an allow or deny rule must declare non-empty values
	// in all hostname fields. Audit-only rules record missing hostnames without
	// denying them.
	//
	// +kubebuilder:validation:MinItems=1
	Hostnames []runtime.ExpressionMatch `json:"hostnames,omitempty"`
}
