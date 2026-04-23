// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

const (
	ResourceScopeNamespace ResourceScope = "Namespace"
	ResourceScopeTenant    ResourceScope = "Tenant"
)

// +kubebuilder:validation:Enum=Namespace;Tenant
type ResourceScope string

func (p ResourceScope) String() string {
	return string(p)
}
