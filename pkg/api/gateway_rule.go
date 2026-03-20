// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GatewayNamespacedName is a namespaced reference to a Gateway resource.
// +kubebuilder:object:generate=true
type GatewayNamespacedName struct {
	// Name of the Gateway.
	Name string `json:"name"`
	// Namespace of the Gateway. When empty, the Route's namespace is assumed.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// AllowedGatewaySpec defines which Gateway resources are allowed to be
// referenced by Routes in a namespace, and an optional default Gateway.
// +kubebuilder:object:generate=true
type AllowedGatewaySpec struct {
	// Default is the Gateway injected as parentRef when no parentRef is specified
	// in a Route resource. The Gateway must also be allowed via Allowed or the
	// LabelSelector.
	// +optional
	Default *GatewayNamespacedName `json:"default,omitempty"`

	// LabelSelector matches Gateway resources by labels.
	// +optional
	metav1.LabelSelector `json:",inline"`

	// Allowed is an explicit list of Gateways (by name and optional namespace)
	// that Routes in this namespace may reference.
	// +optional
	Allowed []GatewayNamespacedName `json:"allowed,omitempty"`
}

// MatchDefault returns true when the given namespace/name matches the configured
// default Gateway.
func (in *AllowedGatewaySpec) MatchDefault(gwNamespace, gwName string) bool {
	if in.Default == nil {
		return false
	}

	return in.Default.Name == gwName && (in.Default.Namespace == "" || in.Default.Namespace == gwNamespace)
}

// MatchGateway returns true when the given Gateway is allowed by this spec.
// A Gateway is allowed if it is the default, appears in the Allowed list, or
// its labels match the LabelSelector.
func (in *AllowedGatewaySpec) MatchGateway(gwNamespace, gwName string, obj client.Object) bool {
	if in.MatchDefault(gwNamespace, gwName) {
		return true
	}

	for _, a := range in.Allowed {
		if a.Name == gwName && (a.Namespace == "" || a.Namespace == gwNamespace) {
			return true
		}
	}

	if obj != nil && (len(in.MatchLabels) > 0 || len(in.MatchExpressions) > 0) {
		selector, err := metav1.LabelSelectorAsSelector(&in.LabelSelector)
		if err == nil && selector.Matches(labels.Set(obj.GetLabels())) {
			return true
		}
	}

	return false
}

// GatewayRuleSpec defines gateway-related enforcement rules for a namespace.
// +kubebuilder:object:generate=true
type GatewayRuleSpec struct {
	// Gateway restricts which Gateways Routes may reference and optionally
	// specifies a default Gateway injected when no parentRef is provided.
	// +optional
	Gateway *AllowedGatewaySpec `json:"gateway,omitempty"`
}
